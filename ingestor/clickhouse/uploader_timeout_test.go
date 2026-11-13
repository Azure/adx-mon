package clickhouse

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

// hangingPrepareConnection models a ClickHouse connection whose PrepareInsert
// hangs forever: the client blocks inside PrepareBatch until its context is
// canceled (for example, a stuck server or a wedged network path).
type hangingPrepareConnection struct {
	fakeConnection

	mu       sync.Mutex
	prepared int
}

func (c *hangingPrepareConnection) PrepareInsert(ctx context.Context, _, _ string, _ []Column) (batchWriter, error) {
	c.mu.Lock()
	c.prepared++
	c.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *hangingPrepareConnection) prepareCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prepared
}

// hangingAppendConnection models a ClickHouse batch whose Append hangs forever
// on the first row (a wedged insert stream), releasing it once its context is
// canceled.
type hangingAppendConnection struct {
	fakeConnection

	mu       sync.Mutex
	appended int
}

func (c *hangingAppendConnection) PrepareInsert(ctx context.Context, _, _ string, _ []Column) (batchWriter, error) {
	return &hangingAppendBatch{ctx: ctx, conn: c}, nil
}

func (c *hangingAppendConnection) appendCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.appended
}

type hangingAppendBatch struct {
	ctx  context.Context
	conn *hangingAppendConnection
}

func (b *hangingAppendBatch) Append(...any) error {
	b.conn.mu.Lock()
	b.conn.appended++
	b.conn.mu.Unlock()
	<-b.ctx.Done()
	return b.ctx.Err()
}

func (b *hangingAppendBatch) Flush() error { return nil }
func (b *hangingAppendBatch) Send() error  { return nil }
func (b *hangingAppendBatch) Close() error { return nil }

// newHangUploader builds an uploader whose single worker is already running
// against the supplied fake connection, with the per-batch upload timeout
// overridden to timeout so the test completes quickly.  The caller owns the
// returned cancel function.
func newHangUploader(t *testing.T, conn connectionManager, timeout time.Duration) (*uploader, context.CancelFunc) {
	t.Helper()
	withFakeConnectionManager(t, conn)

	cfg := Config{Database: "metricsdb", DSN: "clickhouse://localhost"}
	up, err := NewUploader(cfg, nil)
	require.NoError(t, err)
	u := up.(*uploader)

	orig := batchUploadTimeout
	t.Cleanup(func() { batchUploadTimeout = orig })
	batchUploadTimeout = timeout

	ctx, cancel := context.WithCancel(context.Background())
	u.ctx = ctx
	u.jobs = cluster.FairQueue(ctx, u.queue, cluster.FairQueueOpts{Workers: 1})
	u.wg.Add(1)
	go u.worker(0)

	return u, cancel
}

// hangBatch builds a one-row WAL batch for the uploader's database and table.
func hangBatch(t *testing.T, table string) *cluster.Batch {
	t.Helper()
	seg, err := wal.NewSegment(t.TempDir(), "metricsdb_"+table+"_schema")
	require.NoError(t, err)

	timestamp := time.Now().UTC().Truncate(time.Second)
	header := schema.AppendCSVHeader(make([]byte, 0, 128), schema.DefaultMetricsMapping)

	var buf bytes.Buffer
	buf.Write(header)
	fmt.Fprintf(&buf, "%s,0,\"{}\",42.5\n", timestamp.Format(time.RFC3339Nano))
	buf.WriteString(strings.TrimSpace(string(header)))
	buf.WriteByte('\n')

	_, err = seg.Write(context.Background(), buf.Bytes())
	require.NoError(t, err)
	// Close flushes the buffered block to disk so the segment is readable.
	require.NoError(t, seg.Close())

	info := seg.Info()
	return &cluster.Batch{
		Segments: []wal.SegmentInfo{info},
		Database: "metricsdb",
		Table:    table,
		Prefix:   "metricsdb_" + table,
	}
}

// TestWorkerBoundedBatchTimeoutPrepareHangs proves that a batch stuck inside
// PrepareInsert no longer wedges the upload worker forever.  Pre-fix,
// processBatch ran on the service-lifetime context, so the worker blocked in
// PrepareInsert until Close(); the probe batch below was never dequeued and the
// test failed.  With the per-batch bounded context the first batch gives up
// after batchUploadTimeout, the worker retries it, and the probe is scheduled
// promptly.
func TestWorkerBoundedBatchTimeoutPrepareHangs(t *testing.T) {
	const timeout = 100 * time.Millisecond

	conn := &hangingPrepareConnection{}
	u, cancel := newHangUploader(t, conn, timeout)
	defer cancel()

	first := hangBatch(t, "CpuUsage")
	setBatcher(t, first, &stubBatcher{})
	u.queue <- first

	// The first batch must actually reach PrepareInsert before the probe is
	// enqueued, so the probe is the real discriminator between a wedged worker
	// and a recovered one.
	require.Eventually(t, func() bool { return conn.prepareCalls() >= 1 }, time.Second, time.Millisecond)

	probe := hangBatch(t, "MemoryUsage")
	setBatcher(t, probe, &stubBatcher{})
	u.queue <- probe

	// A wedged worker never picks up the probe.  A recovered one does.
	require.Eventually(t, func() bool { return probe.IsReleased() }, 2*timeout+500*time.Millisecond, time.Millisecond,
		"probe batch never scheduled: worker wedged in PrepareInsert with no per-batch timeout")
}

// TestWorkerBoundedBatchTimeoutAppendHangs proves the same for a batch stuck
// inside writer.Append.
func TestWorkerBoundedBatchTimeoutAppendHangs(t *testing.T) {
	const timeout = 100 * time.Millisecond

	conn := &hangingAppendConnection{}
	u, cancel := newHangUploader(t, conn, timeout)
	defer cancel()

	first := hangBatch(t, "CpuUsage")
	setBatcher(t, first, &stubBatcher{})
	u.queue <- first

	require.Eventually(t, func() bool { return conn.appendCalls() >= 1 }, time.Second, time.Millisecond)

	probe := hangBatch(t, "MemoryUsage")
	setBatcher(t, probe, &stubBatcher{})
	u.queue <- probe

	require.Eventually(t, func() bool { return probe.IsReleased() }, 2*timeout+500*time.Millisecond, time.Millisecond,
		"probe batch never scheduled: worker wedged in Append with no per-batch timeout")
}
