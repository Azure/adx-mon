package wal_test

import (
	"context"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

func TestRepository_Write(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}
	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {
			dir := t.TempDir()
			r := wal.NewRepository(wal.RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			require.NoError(t, r.Open(context.Background()))
			w, err := r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)
			require.NoError(t, w.Write(context.Background(), []byte("bar")))
		})
	}
}

func TestRepository_Keys(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}

	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			r := wal.NewRepository(wal.RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			require.NoError(t, r.Open(context.Background()))

			_, err := r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)

			_, err = r.Get(context.Background(), []byte("foo"))
			require.NoError(t, err)

			_, err = r.Get(context.Background(), []byte("bar"))
			require.NoError(t, err)

			keys := r.Keys()
			require.Equal(t, 2, len(keys))
			require.Equal(t, "bar", string(keys[0]))
			require.Equal(t, "foo", string(keys[1]))
		})
	}
}

func TestRepository_Remove(t *testing.T) {
	var providerTests = []struct {
		Name string
	}{
		{Name: "Disk"},
	}
	for _, tt := range providerTests {
		t.Run(tt.Name, func(t *testing.T) {

			dir := t.TempDir()
			r := wal.NewRepository(wal.RepositoryOpts{
				StorageDir: dir,
			})
			defer r.Close()

			// Add a closed segment for this WAL.
			seg, err := wal.NewSegment(dir, "db_foo")
			require.NoError(t, err)
			require.NoError(t, seg.Close())

			require.NoError(t, r.Open(context.Background()))
			w, err := r.Get(context.Background(), []byte("db_foo"))
			require.NoError(t, err)
			require.NoError(t, w.Write(context.Background(), []byte("bar")))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Equal(t, 2, len(entries))

			// Expect an error trying remove WAL that is still open.
			require.Error(t, r.Remove([]byte("db_foo")))

			// WAL must be closed before we can remove it.
			require.NoError(t, w.Close())

			require.NoError(t, r.Remove([]byte("db_foo")))
			require.Equal(t, 0, len(r.Keys()))

			entries, err = os.ReadDir(dir)
			require.NoError(t, err)
			require.Equal(t, 0, len(entries))
		})
	}
}
