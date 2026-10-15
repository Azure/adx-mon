package wal

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSegmentAdmission(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{60, 1}})
	s, err := NewSegment(t.TempDir(), "DB_A_s", WithAdmission(a))
	require.NoError(t, err)
	defer s.Close()
	_, err = s.Write(context.Background(), []byte("hello"))
	require.NoError(t, err)
	require.Equal(t, s.Size(), a.Usage().Bytes)
	_, err = NewSegment(t.TempDir(), "DB_A_other", WithAdmission(a))
	var quota *TableOverloadedError
	require.ErrorAs(t, err, &quota)
	b, err := NewSegment(t.TempDir(), "DB_B_s", WithAdmission(a))
	require.NoError(t, err)
	defer b.Close()
	_, err = b.Write(context.Background(), []byte("healthy"))
	require.NoError(t, err)
}

func TestSegmentCreationRollback(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{100, 1}})
	_, err := NewSegment(t.TempDir()+"/missing", "DB_A_s", WithAdmission(a))
	require.Error(t, err)
	require.Equal(t, Usage{}, a.Usage())
}

func TestWALRotationPreservesMetadataAndBudget(t *testing.T) {
	a := NewAdmission(AdmissionOpts{Default: Usage{1000, 2}})
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxSize: 40, Admission: a})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()
	require.NoError(t, w.Write(context.Background(), []byte("hello"), WithSampleMetadata(LogSampleType, 1)))
	require.NoError(t, w.Write(context.Background(), []byte("hello world hello world"), WithSampleMetadata(LogSampleType, 2)))
	require.EqualValues(t, 2, a.Usage().Segments)
	it, err := w.Segment().Iterator()
	require.NoError(t, err)
	defer it.Close()
	require.NoError(t, w.Flush())
	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	typ, count := it.Metadata()
	require.Equal(t, LogSampleType, typ)
	require.EqualValues(t, 2, count)
}

func BenchmarkSegmentWriteAdmission(b *testing.B) {
	s, err := NewSegment(b.TempDir(), "DB_A_s", WithAdmission(NewAdmission(AdmissionOpts{})))
	require.NoError(b, err)
	defer s.Close()
	payload := []byte("test")
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		written, err := s.Write(context.Background(), payload)
		require.NoError(b, err)
		require.True(b, written > 0)
	}
}

func TestWALLargeBlockCanUseEmptySegment(t *testing.T) {
	for _, budget := range []*Admission{nil, NewAdmission(AdmissionOpts{})} {
		w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxSize: 1, Admission: budget})
		require.NoError(t, err)
		require.NoError(t, w.Open(context.Background()))
		require.NoError(t, w.Write(context.Background(), []byte("larger than the rotation threshold")))
		require.NoError(t, w.Write(context.Background(), []byte("next block")))
		require.Equal(t, 1, w.index.TotalSegments())
		require.NoError(t, w.Close())
	}
}
