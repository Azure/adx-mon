package wal

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// A segment closed while writers are writing to it, as rotation does, reports every write that succeeded in its size
// and uncompressed size.
func TestSegmentInfoCountsWritesRacingClose(t *testing.T) {
	for i := 0; i < 500; i++ {
		s, err := NewSegment(t.TempDir(), "db_T")
		require.NoError(t, err)
		var mu sync.Mutex
		var written int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				v := []byte("row,1234567890\n")
				for {
					if _, err := s.Write(context.Background(), v); err != nil {
						return
					}
					mu.Lock()
					written += int64(len(v))
					mu.Unlock()
				}
			}()
		}
		close(start)
		for {
			mu.Lock()
			w := written
			mu.Unlock()
			if w > 200 {
				break
			}
		}
		require.NoError(t, s.Close())
		info := s.Info()
		wg.Wait()
		fi, err := os.Stat(s.Path())
		require.NoError(t, err)
		require.Equal(t, written, info.RawSize, "iteration %d", i)
		require.Equal(t, fi.Size(), info.Size, "iteration %d", i)
	}
}
