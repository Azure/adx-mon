package gzip

import (
	"os"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

func TestCompressReaderCloseStopsGoroutine(t *testing.T) {
	path := t.TempDir() + "/data.txt"
	require.NoError(t, os.WriteFile(path, []byte("hello world, this is a small file for the compress reader"), 0644))
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	synctest.Test(t, func(t *testing.T) {
		r := NewCompressReader(f)
		defer r.Close()
		// Stop consuming before the writer can finish its compressed stream.
		_, err := r.Read(make([]byte, 4))
		require.NoError(t, err)
		synctest.Wait()
		_, err = f.Stat()
		require.NoError(t, err, "the gated compression goroutine still owns the file")
		require.NoError(t, r.Close())
		synctest.Wait()
		_, err = f.Stat()
		require.ErrorIs(t, err, os.ErrClosed, "writer cleanup must release its source file")
	})
}
