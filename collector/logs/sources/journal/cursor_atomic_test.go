package journal

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// A cursor is always readable while it is rewritten.  Otherwise a crash during the rewrite leaves an unreadable
// cursor and the journal's retained history is replayed, duplicating its logs.
func TestWriteCursorIsAtomic(t *testing.T) {
	const writes = 20000
	path := filepath.Join(t.TempDir(), "journal.cursor")
	writeCursor(path, "c0")

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
			if _, err := readCursor(path); err != nil {
				unreadable.Add(1)
			}
		}
	})
	for i := 1; i <= writes; i++ {
		writeCursor(path, fmt.Sprintf("c%d", i))
	}
	close(stop)
	wg.Wait()

	t.Logf("cursor was unreadable %d times during %d rewrites", unreadable.Load(), writes)
	require.Zero(t, unreadable.Load())
	cursor, err := readCursor(path)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("c%d", writes), cursor)
}
