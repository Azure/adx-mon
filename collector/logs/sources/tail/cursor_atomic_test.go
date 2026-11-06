package tail

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A cursor is always readable while it is rewritten.  Otherwise a crash during the rewrite leaves an unreadable
// cursor and the file is tailed again from the start, duplicating its logs.
func TestWriteCursorIsAtomic(t *testing.T) {
	const writes = 20000
	path := filepath.Join(t.TempDir(), "file.cursor")
	require.NoError(t, writeCursor(path, "fid", 0))

	var unreadable atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, _, err := readCursor(path); err != nil {
				unreadable.Add(1)
			}
		}
	})
	for i := 1; i <= writes; i++ {
		require.NoError(t, writeCursor(path, "fid", int64(i)))
	}
	close(stop)
	wg.Wait()

	t.Logf("cursor was unreadable %d times during %d rewrites", unreadable.Load(), writes)
	require.Zero(t, unreadable.Load())
	_, cursor, err := readCursor(path)
	require.NoError(t, err)
	require.EqualValues(t, writes, cursor)
}
