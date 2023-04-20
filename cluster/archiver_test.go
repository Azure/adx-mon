package cluster

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiver_ClosedSegments(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "Cpu_aaaa.csv"))
	require.NoError(t, err)
	defer f.Close()

	f1, err := os.Create(filepath.Join(dir, "Cpu_bbbb.csv"))
	require.NoError(t, err)
	defer f1.Close()

	a := &archiver{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node1"},
		Segmenter:   &fakeSegmenter{active: filepath.Join(dir, "Cpu_bbbb.csv")},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(dir, "Cpu_aaaa.csv")}, owner[0])
	require.Equal(t, 0, len(notOwned))
}

func TestArchiver_NodeOwned(t *testing.T) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, "Cpu_aaaa.csv"))
	require.NoError(t, err)
	defer f.Close()

	f1, err := os.Create(filepath.Join(dir, "Cpu_bbbb.csv"))
	require.NoError(t, err)
	defer f1.Close()

	a := &archiver{
		hostname:    "node1",
		storageDir:  dir,
		Partitioner: &fakePartitioner{owner: "node2"},
		Segmenter:   &fakeSegmenter{active: "Memory_aaaa.csv"},
	}
	owner, notOwned, err := a.processSegments()
	require.NoError(t, err)
	require.Equal(t, 0, len(owner))
	require.Equal(t, []string{filepath.Join(dir, "Cpu_aaaa.csv"), filepath.Join(dir, "Cpu_bbbb.csv")}, notOwned[0])
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
