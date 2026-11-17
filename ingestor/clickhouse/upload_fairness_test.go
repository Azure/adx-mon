package clickhouse

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

type slowTableConnection struct {
	fakeConnection
	started chan string
	gate    <-chan struct{}
}

func (c *slowTableConnection) PrepareInsert(ctx context.Context, _, table string, _ []Column) (batchWriter, error) {
	c.started <- table
	if table == "A" {
		// Keep A stalled until the test has proved that B can finish.
		select {
		case <-ctx.Done():
		case <-c.gate:
		}
	}
	return nil, context.DeadlineExceeded
}

func TestClickHouseWorkersIsolateSlowTable(t *testing.T) {
	// Generate flake IDs on the real clock, before entering synctest's clock.
	makeBatch := func(table string) *cluster.Batch {
		seg, err := wal.NewSegment(t.TempDir(), "DB_"+table+"_s")
		require.NoError(t, err)
		data := schema.AppendCSVHeader(nil, schema.DefaultMetricsMapping)
		data = append(data, fmt.Sprintf("%s,0,\"{}\",1\n", time.Now().UTC().Format(time.RFC3339Nano))...)
		_, err = seg.Write(context.Background(), data)
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		b := &cluster.Batch{Database: "DB", Table: table, Segments: []wal.SegmentInfo{seg.Info()}}
		setBatcher(t, b, &stubBatcher{})
		return b
	}
	a, nextA, b := makeBatch("A"), makeBatch("A"), makeBatch("B")
	synctest.Test(t, func(t *testing.T) {
		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		conn := &slowTableConnection{started: make(chan string, 10), gate: gate}
		withFakeConnectionManager(t, conn)
		up, err := NewUploader(Config{Database: "DB", DSN: "clickhouse://localhost"}, nil)
		require.NoError(t, err)
		u := up.(*uploader)
		ctx, cancel := context.WithCancel(context.Background())
		u.ctx = ctx
		u.jobs = cluster.FairQueue(ctx, u.queue, cluster.FairQueueOpts{Workers: 2})
		for n := range 2 {
			u.wg.Add(1)
			go u.worker(n)
		}
		defer func() { cancel(); release(); u.wg.Wait() }()
		u.queue <- a
		synctest.Wait()
		select {
		case table := <-conn.started:
			require.Equal(t, "A", table)
		default:
			t.Fatal("A did not start")
		}
		require.False(t, a.IsReleased(), "A must remain gated")
		u.queue <- nextA
		u.queue <- b
		synctest.Wait()
		select {
		case table := <-conn.started:
			require.Equal(t, "B", table)
		default:
			t.Fatal("B starved while A was gated")
		}
		require.True(t, b.IsReleased(), "B must finish before the test releases A")
		require.False(t, b.IsRemoved())
		require.False(t, a.IsReleased(), "B must not depend on A timing out")
		require.False(t, nextA.IsReleased(), "the second A must still be queued")
		cancel()
		release()
		u.wg.Wait()
		synctest.Wait()
		require.True(t, a.IsReleased())
		require.True(t, nextA.IsReleased())
	})
}
