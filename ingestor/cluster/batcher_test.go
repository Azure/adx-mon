package cluster

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestBatcher_ClosedSegments(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, wal.Filename("db", "Cpu", "aaaa")))
	require.NoError(t, err)
	defer f.Close()

	f1, err := os.Create(filepath.Join(dir, wal.Filename("db", "Cpu", "bbbb")))
	require.NoError(t, err)
	defer f1.Close()

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   &fakeSegmenter{active: filepath.Join(dir, wal.Filename("db", "Cpu", "bbbb"))},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Cpu", "aaaa"))}, owner[0])
	require.Equal(t, 0, len(notOwned))
}

func TestBatcher_NodeOwned(t *testing.T) {
	dir := t.TempDir()

	idgen, err := flake.New()
	require.NoError(t, err)
	now := idgen.NextId()

	fName := wal.Filename("db", "Cpu", now.String())
	f, err := os.Create(filepath.Join(dir, fName))
	require.NoError(t, err)
	defer f.Close()

	now = idgen.NextId()
	f1Name := wal.Filename("db", "Cpu", now.String())
	f1, err := os.Create(filepath.Join(dir, f1Name))
	require.NoError(t, err)
	defer f1.Close()

	a := &batcher{
		hostname:       "node1",
		storageDir:     dir,
		maxSegmentAge:  30 * time.Second,
		maxSegmentSize: 100 * 1024 * 1024,
		Partitioner:    &fakePartitioner{owner: "node2"},
		Segmenter:      &fakeSegmenter{active: wal.Filename("db", "Memory", "aaaa")},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 0, len(owner))
	require.Equal(t, []string{filepath.Join(dir, fName), filepath.Join(dir, f1Name)}, notOwned[0])
}

func TestBatcher_OldestFirst(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac7d6f0001")))
	require.NoError(t, err)
	defer f.Close()

	// This segment is older, but lexicographically greater.  It should be the in the first batch.
	f1, err := os.Create(filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001")))
	require.NoError(t, err)
	defer f1.Close()

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   &fakeSegmenter{active: wal.Filename("db", "Memory", "aaaa")},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 2, len(owner))
	require.Equal(t, 0, len(notOwned))
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Disk", "2359cd7e3aef0001"))}, owner[0])
	require.Equal(t, []string{filepath.Join(dir, wal.Filename("db", "Cpu", "2359cdac7d6f0001"))}, owner[1])
}

type fakePartitioner struct {
	owner string
}

func (f *fakePartitioner) Owner(b []byte) (string, string) {
	return f.owner, ""
}

type fakeSegmenter struct {
	active string
}

func (f *fakeSegmenter) IsActiveSegment(path string) bool {
	return path == f.active
}
