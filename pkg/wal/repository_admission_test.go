package wal

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestRepositoryRestoresAndDrainsOverQuota(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSegment(dir, "DB_A_s")
	require.NoError(t, err)
	_, err = s.Write(context.Background(), []byte("persisted data"))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	size := s.Size()
	a := NewAdmission(AdmissionOpts{Enforce: true, Default: Usage{size - 1, 1}})
	r := NewRepository(RepositoryOpts{StorageDir: dir, Admission: a})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, r.Open(ctx))
	defer r.Close()
	require.Equal(t, Usage{size, 1}, a.Usage())
	w, err := r.Get(ctx, []byte("DB_A_s"))
	require.NoError(t, err)
	var quota *TableOverloadedError
	require.ErrorAs(t, w.Write(ctx, []byte("new")), &quota)
	require.NoError(t, w.Remove(s.Path()))
	r.Index().Remove(s.Info())
	require.Equal(t, Usage{}, a.Usage())
	require.NoError(t, w.Write(ctx, []byte("new")))
}

func TestFailedRemovalKeepsAdmission(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nonempty"
	require.NoError(t, os.Mkdir(path, 0700))
	require.NoError(t, os.WriteFile(path+"/child", nil, 0600))
	a := NewAdmission(AdmissionOpts{})
	a.Restore(path, Destination{"DB", "A"}, 10)
	w, err := NewWAL(WALOpts{StorageDir: dir, Prefix: "DB_A", Admission: a})
	require.NoError(t, err)
	require.Error(t, w.Remove(path))
	require.Equal(t, Usage{10, 1}, a.Usage())
}
