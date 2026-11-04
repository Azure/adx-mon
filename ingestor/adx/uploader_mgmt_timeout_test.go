package adx

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// A stalled table or mapping management command fails the batch after the short schema timeout instead of holding
// the upload worker for the full batch upload timeout.
func TestUploadReaderSchemaCommandsTimeOutQuickly(t *testing.T) {
	defer func(d time.Duration) { schemaMgmtTimeout = d }(schemaMgmtTimeout)
	schemaMgmtTimeout = 100 * time.Millisecond

	mgmt := &blockingMgmt{block: make(chan struct{}), blockOn: "Slow", calls: make(chan string, 16)}
	defer close(mgmt.block)
	u := &uploader{database: "DB", syncer: NewSyncer(mgmt, "DB", schema.DefaultMetricsMapping, PromMetrics)}

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- u.uploadReader(context.Background(), strings.NewReader("payload"), "DB", "Slow", schema.DefaultMetricsMapping)
	}()

	select {
	case err := <-done:
		t.Logf("stalled schema command released the worker after %s", time.Since(start).Round(10*time.Millisecond))
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		t.Fatalf("worker still blocked on a stalled schema command after %s", time.Since(start).Round(time.Second))
	}
}

// Realtime batches get the same bound: a stalled schema command falls back to queued ingestion after the short schema
// timeout instead of holding the realtime worker for the full batch upload timeout.
func TestStreamBatchSchemaCommandsTimeOutQuickly(t *testing.T) {
	defer func(d time.Duration) { schemaMgmtTimeout = d }(schemaMgmtTimeout)
	schemaMgmtTimeout = 100 * time.Millisecond

	env := newStreamingTestEnv(t)
	mgmt := &blockingMgmt{block: make(chan struct{}), blockOn: "Cpu", calls: make(chan string, 16)}
	defer close(mgmt.block)
	env.u.syncer = NewSyncer(mgmt, "db", schema.DefaultMetricsMapping, PromMetrics)
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
	batch := env.nextBatch(t, ingestpolicy.PriorityRealtime)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		env.u.uploadBatch(context.Background(), batch)
	}()

	select {
	case <-done:
		t.Logf("stalled schema command released the realtime worker after %s", time.Since(start).Round(10*time.Millisecond))
		require.Zero(t, env.stream.calls)
		require.Equal(t, 1, env.queue.calls, "batch should fall back to queued ingestion")
	case <-time.After(3 * time.Second):
		t.Fatalf("realtime worker still blocked on a stalled schema command after %s", time.Since(start).Round(time.Second))
	}
}
