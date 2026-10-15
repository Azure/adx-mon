package wal

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// failingSegment closes with an error, simulating a failed flush or fsync
// during rotation.
type failingSegment struct {
	Segment
	path   string
	size   int64
	closed bool
}

func (s *failingSegment) Close() error {
	s.closed = true
	return errors.New("fsync failed")
}
func (s *failingSegment) Path() string         { return s.path }
func (s *failingSegment) Size() int64          { return s.size }
func (s *failingSegment) CreatedAt() time.Time { return time.Now().Add(-time.Hour) }
func (s *failingSegment) Info() SegmentInfo {
	return SegmentInfo{Prefix: "DB_A_s", Ulid: "1", Path: s.path, Size: s.size, CreatedAt: s.CreatedAt()}
}
func (s *failingSegment) Flush() error                   { return nil }
func (s *failingSegment) Bytes() ([]byte, error)         { return nil, nil }
func (s *failingSegment) Reader() (io.ReadCloser, error) { return nil, nil }
func (s *failingSegment) Iterator() (Iterator, error)    { return nil, nil }
func (s *failingSegment) Repair() error                  { return nil }
func (s *failingSegment) ID() string                     { return "1" }
func (s *failingSegment) Write(context.Context, []byte, ...WriteOptions) (int, error) {
	return 0, nil
}
func (s *failingSegment) Append(context.Context, []byte) (int, error) { return 0, nil }

// A segment that cannot be closed still holds data on disk and still consumes
// the table's budget, so it must remain discoverable by the uploader.
func TestRotationIndexesSegmentWhenCloseFails(t *testing.T) {
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxAge: time.Minute})
	require.NoError(t, err)

	failing := &failingSegment{path: "DB_A_s_1.wal", size: 4096}
	w.segment = failing
	w.segmentCreatedAt = time.Now().Add(-time.Hour).Unix()

	w.rotateSegmentIfNecessary()

	require.True(t, failing.closed)
	require.NotEqual(t, Segment(failing), w.Segment(), "rotation moved off the failed segment")
	require.Equal(t, 1, w.index.TotalSegments(), "backlog remains discoverable after a failed close")
	require.Equal(t, failing.Info().Path, w.index.Get(nil, "DB_A_s")[0].Path)
}

// An empty segment that fails to close carries no data and must not be indexed
// as an uploadable backlog entry.
func TestRotationSkipsEmptySegmentWhenCloseFails(t *testing.T) {
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxAge: time.Minute})
	require.NoError(t, err)

	w.segment = &failingSegment{path: "DB_A_s_1.wal", size: 8}
	w.segmentCreatedAt = time.Now().Add(-time.Hour).Unix()

	w.rotateSegmentIfNecessary()

	require.Zero(t, w.index.TotalSegments())
}

// Normal rotation must continue to index the closed segment and keep writing.
func TestRotationIndexesAndContinuesOnSuccess(t *testing.T) {
	a := NewAdmission(AdmissionOpts{})
	w, err := NewWAL(WALOpts{StorageDir: t.TempDir(), Prefix: "DB_A_s", SegmentMaxSize: 24, Admission: a})
	require.NoError(t, err)
	require.NoError(t, w.Open(context.Background()))
	defer w.Close()

	require.NoError(t, w.Write(context.Background(), []byte("first block of data")))
	require.NoError(t, w.Write(context.Background(), []byte("second block of data")))

	require.Equal(t, 1, w.index.TotalSegments(), "rotated segment is indexed")
	require.NotNil(t, w.Segment(), "writing continues after rotation")
	require.EqualValues(t, 2, a.Usage().Segments, "both segments remain charged")
}
