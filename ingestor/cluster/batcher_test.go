package cluster

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArchiver_ClosedSegments(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "Cpu_aaaa.wal"))
	require.NoError(t, err)
	defer f.Close()

	f1, err := os.Create(filepath.Join(dir, "Cpu_bbbb.wal"))
	require.NoError(t, err)
	defer f1.Close()

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   &fakeSegmenter{active: filepath.Join(dir, "Cpu_bbbb.wal")},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, "Cpu_aaaa.wal")}, owner[0])
	require.Equal(t, 0, len(notOwned))
}

func TestArchiver_NodeOwned(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "Cpu_aaaa.wal"))
	require.NoError(t, err)
	defer f.Close()

	f1, err := os.Create(filepath.Join(dir, "Cpu_bbbb.wal"))
	require.NoError(t, err)
	defer f1.Close()

	a := &batcher{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node2"},
		Segmenter:   &fakeSegmenter{active: "Memory_aaaa.wal"},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 0, len(owner))
	require.Equal(t, []string{filepath.Join(dir, "Cpu_aaaa.wal"), filepath.Join(dir, "Cpu_bbbb.wal")}, notOwned[0])
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
