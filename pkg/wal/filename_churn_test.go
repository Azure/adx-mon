package wal

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Model many short-lived unowned table segments, mixing continuous traffic
// with tables that receive a burst every five rotation periods. Drive deadlines
// explicitly so counts do not depend on scheduler timing or filesystem speed.
// Both local writes and imported, already compressed blocks use this lifecycle.
func TestWAL_FilenameChurn(t *testing.T) {
	ctx := context.Background()
	source := newTestWAL(t, WALOpts{})
	require.NoError(t, source.Write(ctx, []byte("imported\n")))
	require.NoError(t, source.Flush())
	imported, err := os.ReadFile(source.Path())
	require.NoError(t, err)

	var created atomic.Int64
	testHookSegmentCreated = func() { created.Add(1) }
	defer func() { testHookSegmentCreated = nil }()
	dir := t.TempDir()
	admission := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{Bytes: 1 << 20, Segments: 2}})
	scheduler := newRotationScheduler(0, nil) // deliberately not started
	const hot, sparse, periods = 32, 18, 50
	wals := make([]*WAL, hot+sparse)
	for i := range wals {
		wals[i], err = NewWAL(WALOpts{
			StorageDir: dir, Prefix: fmt.Sprintf("DB_T%d_schema", i),
			SegmentMaxAge: time.Second, Admission: admission, scheduler: scheduler,
		})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, wals[i].Close()) })
	}
	dataSegments, removedData := 0, 0
	for tick := range periods + 1 { // final period drains speculative successors
		for i, w := range wals {
			want := fmt.Sprintf("local:%d\n", i)
			if i%2 != 0 {
				want = "imported\n"
			}
			if tick < periods && (i < hot || tick%5 == 0) {
				if i%2 == 0 {
					require.NoError(t, w.Write(ctx, []byte(want)))
				} else {
					require.NoError(t, w.Append(ctx, imported))
				}
				dataSegments++
			}
			if w.Segment() != nil {
				// Use the same monotonic time domain as requiresRotation.
				atomic.StoreInt64(&w.segmentCreatedAt, monoNow()-int64(2*time.Second))
				w.rotateSegmentIfNecessary()
			}
			for _, info := range w.index.Get(nil, w.opts.Prefix) {
				reader, err := NewSegmentReader(info.Path)
				require.NoError(t, err)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.Equal(t, want, string(data))
				require.NoError(t, reader.Close())
				require.NoError(t, w.Remove(info.Path))
				w.index.Remove(info)
				removedData++
			}
		}
	}
	require.Equal(t, dataSegments, removedData)
	require.Empty(t, walFiles(t, dir))
	require.Equal(t, Usage{}, admission.Usage())
	// Each successful newSegment call created one unique file. With no files
	// remaining, the same number were unlinked (including internal empty deletes).
	t.Logf("creates=%d unlinks=%d data_segments=%d empty_segments=%d",
		created.Load(), created.Load(), dataSegments, created.Load()-int64(dataSegments))
	require.EqualValues(t, dataSegments, created.Load(), "rotation without pending writes must not create empty successors")
}

// Rotation leaves no active segment, but its data is still durable and
// discoverable after restart. A reader opened before a later lazy append keeps
// the original file identity and sees only its own blocks.
func TestWAL_DeferredSuccessorRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	w, err := NewWAL(WALOpts{
		StorageDir: dir, Prefix: "DB_A_schema", SegmentMaxAge: time.Hour,
		EnableWALFsync: true, Admission: NewAdmission(AdmissionOpts{}),
		scheduler: newRotationScheduler(0, nil),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	require.NoError(t, w.Write(ctx, []byte("first"), WithSampleMetadata(LogSampleType, 1)))
	first := w.Path()
	expireSegment(w)
	w.rotateSegmentIfNecessary()
	require.Nil(t, w.Segment())
	reader, err := NewSegmentReader(first)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, w.Write(ctx, []byte("second"), WithSampleMetadata(LogSampleType, 2)))
	require.NotEqual(t, first, w.Path())
	require.NoError(t, w.Close())
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "first", string(data))

	admission := NewAdmission(AdmissionOpts{})
	r := NewRepository(RepositoryOpts{StorageDir: dir, Admission: admission})
	require.NoError(t, r.Open(ctx))
	defer r.Close()
	require.EqualValues(t, 2, admission.Usage().Segments)
	infos := r.Index().Get(nil, "DB_A_schema")
	require.Len(t, infos, 2)
	got := map[string]uint32{}
	for _, info := range infos {
		reader, err := NewSegmentReader(info.Path)
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		typ, count := reader.SampleMetadata()
		require.Equal(t, LogSampleType, typ)
		got[string(data)] = count
		require.NoError(t, reader.Close())
	}
	require.Equal(t, map[string]uint32{"first": 1, "second": 2}, got)
}

// Measure the tradeoff separately from contended writes: with no pending
// writer, the first write after rotation now includes segment creation.
func BenchmarkWAL_WriteAfterRotation(b *testing.B) {
	w, err := NewWAL(WALOpts{
		StorageDir: b.TempDir(), Prefix: "DB_A_schema", SegmentMaxAge: time.Second,
		scheduler: newRotationScheduler(0, nil),
	})
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, w.Close()) })
	ctx := context.Background()
	payload := []byte("payload")
	require.NoError(b, w.Write(ctx, payload))
	latencies := make([]time.Duration, 0, b.N)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		atomic.StoreInt64(&w.segmentCreatedAt, monoNow()-int64(2*time.Second))
		w.rotateSegmentIfNecessary()
		for _, info := range w.index.Get(nil, w.opts.Prefix) {
			require.NoError(b, w.Remove(info.Path))
			w.index.Remove(info)
		}
		b.StartTimer()
		start := time.Now()
		err := w.Write(ctx, payload)
		latencies = append(latencies, time.Since(start))
		require.NoError(b, err)
	}
	b.StopTimer()
	slices.Sort(latencies)
	b.ReportMetric(float64(latencies[len(latencies)/2].Nanoseconds()), "p50-ns")
	b.ReportMetric(float64(latencies[len(latencies)*99/100].Nanoseconds()), "p99-ns")
}
