package wal

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func rotationMetric(t *testing.T, table, step string) *dto.Histogram {
	t.Helper()
	var metric dto.Metric
	h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", step, "db", table, "success")
	require.NoError(t, h.(prometheus.Metric).Write(&metric))
	return metric.GetHistogram()
}

func TestPipelineMetrics_AgeRotationWithoutSweep(t *testing.T) {
	s := newRotationScheduler(0, nil)
	s.Open(context.Background())
	t.Cleanup(s.Close)
	w := newTestWAL(t, WALOpts{
		Component: "ingestor", Prefix: "db_Deadline", SegmentMaxAge: 20 * time.Millisecond, scheduler: s,
	})
	before := rotationMetric(t, "Deadline", "wal_rotation_lateness").GetSampleCount()
	require.NoError(t, w.Write(context.Background(), []byte("data")))
	require.Eventually(t, func() bool {
		return rotationMetric(t, "Deadline", "wal_rotation_lateness").GetSampleCount() == before+1 && w.index.TotalSegments() == 1
	}, time.Second, time.Millisecond)
	infos := w.index.Get(nil, w.opts.Prefix)
	require.Len(t, infos, 1)
	require.NotNil(t, infos[0].Timing)
}

func TestPipelineMetrics_OverdueBeforeRotation(t *testing.T) {
	// A scheduler that has not started lets us observe overdue active data, before a completion histogram exists.
	s := newRotationScheduler(0, nil)
	w := newTestWAL(t, WALOpts{
		Component: "ingestor", Prefix: "db_Overdue", SegmentMaxAge: time.Millisecond, scheduler: s,
	})
	require.NoError(t, w.Write(context.Background(), []byte("data")))
	deadline := w.Segment().CreatedAt().Add(w.opts.SegmentMaxAge)
	time.Sleep(time.Until(deadline.Add(10 * time.Millisecond)))
	active, delay := w.rotationStatus(time.Now())
	require.True(t, active)
	require.GreaterOrEqual(t, delay, 10*time.Millisecond)

	before := rotationMetric(t, "Overdue", "wal_rotation_lateness")
	w.rotateSegmentIfNecessary()
	after := rotationMetric(t, "Overdue", "wal_rotation_lateness")
	require.Equal(t, before.GetSampleCount()+1, after.GetSampleCount())
	require.GreaterOrEqual(t, after.GetSampleSum()-before.GetSampleSum(), 0.01)
	_, delay = w.rotationStatus(time.Now())
	require.Zero(t, delay)
}

func TestPipelineMetrics_SizeRotationDoesNotReportAgeLateness(t *testing.T) {
	w := newTestWAL(t, WALOpts{
		Component: "ingestor", Prefix: "db_Size", SegmentMaxAge: time.Hour, SegmentMaxSize: 1024,
	})
	before := rotationMetric(t, "Size", "wal_rotation_lateness").GetSampleCount()
	require.NoError(t, w.Write(context.Background(), []byte("data")))
	atomic.StoreInt64(&w.segmentSize, w.opts.SegmentMaxSize)
	w.rotateSegmentIfNecessary()
	require.Equal(t, 1, w.index.TotalSegments())
	require.Equal(t, before, rotationMetric(t, "Size", "wal_rotation_lateness").GetSampleCount())
}

type blockedCloseSegment struct {
	Segment
	entered chan struct{}
	release chan struct{}
}

func (s blockedCloseSegment) Close() error {
	close(s.entered)
	<-s.release
	return s.Segment.Close()
}

func TestPipelineMetrics_RotationWorkerSaturation(t *testing.T) {
	s := newRotationScheduler(0, nil)
	s.concurrency = 1
	first := newTestWAL(t, WALOpts{Component: "ingestor", Prefix: "db_First", SegmentMaxAge: time.Millisecond, scheduler: s})
	second := newTestWAL(t, WALOpts{Component: "ingestor", Prefix: "db_Second", SegmentMaxAge: time.Millisecond, scheduler: s})
	require.NoError(t, first.Write(context.Background(), []byte("data")))
	require.NoError(t, second.Write(context.Background(), []byte("data")))
	blocked := blockedCloseSegment{Segment: first.Segment(), entered: make(chan struct{}), release: make(chan struct{})}
	first.mu.Lock()
	first.segment = blocked
	first.mu.Unlock()
	var once sync.Once
	unblock := func() { once.Do(func() { close(blocked.release) }) }
	t.Cleanup(unblock)
	time.Sleep(2 * time.Millisecond)
	before := rotationMetric(t, "Second", "wal_rotation_lateness")
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.rotate([]*WAL{first, second})
	}()
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("first rotation did not start")
	}
	time.Sleep(10 * time.Millisecond)
	require.Zero(t, second.index.TotalSegments())
	require.Equal(t, before.GetSampleCount(), rotationMetric(t, "Second", "wal_rotation_lateness").GetSampleCount())
	active, overdue := second.rotationStatus(time.Now())
	require.True(t, active)
	require.GreaterOrEqual(t, overdue, 10*time.Millisecond)
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rotations did not finish")
	}
	after := rotationMetric(t, "Second", "wal_rotation_lateness")
	require.Equal(t, before.GetSampleCount()+1, after.GetSampleCount())
	require.GreaterOrEqual(t, after.GetSampleSum()-before.GetSampleSum(), 0.01)
	require.Equal(t, 1, second.index.TotalSegments())
}

func TestPipelineMetrics_RecoveryRetainsTotalNotStageHistory(t *testing.T) {
	dir := t.TempDir()
	seg, err := NewSegment(dir, "db_Recovered")
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), []byte("data"))
	require.NoError(t, err)
	info := seg.Info()
	require.NoError(t, seg.Close())
	r := NewRepository(RepositoryOpts{StorageDir: dir, Component: "ingestor"})
	require.NoError(t, r.Open(context.Background()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	infos := r.Index().Get(nil, info.Prefix)
	require.Len(t, infos, 1)
	require.NotNil(t, infos[0].Timing)
	now := time.Now()
	infos[0].Timing.Start(now, now)
	got, ok := infos[0].Timing.Complete(now.Add(time.Second))
	require.True(t, ok)
	require.Equal(t, "recovered", got.Incomplete)
	require.InDelta(t, now.Add(time.Second).Sub(info.CreatedAt).Seconds(), got.Total.Seconds(), 1e-6)
}

func TestPipelineMetrics_ActiveWALGauge(t *testing.T) {
	r := NewRepository(RepositoryOpts{StorageDir: t.TempDir(), Component: "ingestor", SegmentMaxAge: time.Second})
	// Avoid the periodic sampler to keep this check deterministic.
	w, err := NewWAL(r.walOpts("db_Active"))
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	t.Cleanup(func() { require.NoError(t, w.Close()) })
	require.NoError(t, w.Write(context.Background(), []byte("data")))
	r.wals.Set("db_Active", w)
	atomic.StoreInt64(&w.segmentCreatedAt, time.Now().Add(-3*time.Second).UnixNano())
	r.sampleWALsOnce()
	var metric dto.Metric
	require.NoError(t, metrics.PipelineActiveSegments.WithLabelValues("ingestor").Write(&metric))
	require.Equal(t, float64(1), metric.GetGauge().GetValue())
	require.NoError(t, metrics.PipelineRotationOverdueSeconds.WithLabelValues("ingestor").Write(&metric))
	require.GreaterOrEqual(t, metric.GetGauge().GetValue(), float64(2))
	require.NoError(t, w.Close())
	r.sampleWALsOnce()
	require.NoError(t, metrics.PipelineActiveSegments.WithLabelValues("ingestor").Write(&metric))
	require.Zero(t, metric.GetGauge().GetValue())
	require.NoError(t, metrics.PipelineRotationOverdueSeconds.WithLabelValues("ingestor").Write(&metric))
	require.Zero(t, metric.GetGauge().GetValue())
}

func BenchmarkWAL_PipelineTiming(b *testing.B) {
	for _, component := range []string{"", "ingestor"} {
		b.Run("component="+component, func(b *testing.B) {
			w, err := NewWAL(WALOpts{StorageDir: b.TempDir(), Prefix: "db_Bench", Component: component})
			require.NoError(b, err)
			require.NoError(b, w.Open(context.Background()))
			b.Cleanup(func() { require.NoError(b, w.Close()) })
			buf := []byte("data\n")
			require.NoError(b, w.Write(context.Background(), buf))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := w.Write(context.Background(), buf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
