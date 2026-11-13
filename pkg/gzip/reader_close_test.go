package gzip

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCompressReaderCloseStopsGoroutine demonstrates the H5 leak: the writer
// goroutine started by NewCompressReader blocks in io.Copy until the pipe
// reader is drained or closed. If the consumer stops reading (e.g. an error
// path discards the reader), nothing unblocks the goroutine, so it leaks
// along with the open *os.File.
//
// Before the fix, CompressReader has no Close method (this test fails to
// compile with "r.Close undefined"), and even if Close existed but failed to
// unblock the writer, the goroutine-count assertion below would fail.
func TestCompressReaderCloseStopsGoroutine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/data.txt", []byte("hello world, this is a small file for the compress reader"), 0644))

	f, err := os.Open(dir + "/data.txt")
	require.NoError(t, err)

	r := NewCompressReader(f)

	// Read only a small prefix so the writer goroutine is still blocked
	// writing compressed data into the pipe via io.Copy.
	_, err = r.Read([]byte(make([]byte, 4)))
	require.NoError(t, err)

	// Settle so the runtime reaches a steady goroutine count before we
	// measure the baseline (which includes the blocked writer goroutine).
	time.Sleep(10 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	// The fix: closing the pipe reader unblocks the writer goroutine, which
	// returns io.ErrClosedPipe from io.Copy, runs its deferred closes (file,
	// pipe writer, gzip writer) and exits.
	require.NoError(t, r.Close())

	// Poll for the goroutine count to drop back to baseline (the compression
	// goroutine exited) and hold there for a stable interval.
	deadline := time.Now().Add(2 * time.Second)
	stableFor := 0
	const stablePolls = 5
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for time.Now().Before(deadline) {
		<-tick.C
		if runtime.NumGoroutine() <= baseline {
			stableFor++
			if stableFor >= stablePolls {
				return
			}
		} else {
			stableFor = 0
		}
	}
	t.Fatalf("compression goroutine did not exit after Close: goroutine count never returned to baseline %d within deadline", baseline)
}
