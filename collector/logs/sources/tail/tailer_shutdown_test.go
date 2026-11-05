package tail

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"github.com/tenebris-tech/tail"
)

// When storage is full the batcher stops reading at shutdown while the queue to it is full.  The line reader must
// still exit, or Tailer.Wait, and RemoveTarget with it, would hang forever.
func TestReadLinesExitsOnShutdownWhenQueueIsFull(t *testing.T) {
	lines := make(chan *tail.Line, 1)
	lines <- &tail.Line{Text: "blocked"}
	tailer := &Tailer{
		tail:          &tail.Tail{Filename: "test.log", Lines: lines},
		logTypeParser: sourceparse.GetLogTypeParser(sourceparse.LogTypePlain),
	}
	// A full queue that nothing reads, like batchQueue once BatchLogs has returned.
	full := make(chan *types.Log)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		readLines(ctx, tailer, nil, full)
	}()

	require.Eventually(t, func() bool { return len(lines) == 0 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("readLines did not exit after shutdown while its output queue was full")
	}
}
