package clickhouse

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type slowTableConnection struct {
	fakeConnection
	started chan string
}

func (c *slowTableConnection) PrepareInsert(ctx context.Context, _, table string, _ []Column) (batchWriter, error) {
	c.started <- table
	if table == "A" {
		<-ctx.Done()
	}
	return nil, context.DeadlineExceeded
}

func TestClickHouseWorkersIsolateSlowTable(t *testing.T) {
	conn := &slowTableConnection{started: make(chan string, 10)}
	original := newConnectionManager
	newConnectionManager = func(Config) (connectionManager, error) { return conn, nil }
	defer func() { newConnectionManager = original }()
	up, err := NewUploader(Config{Database: "DB", DSN: "clickhouse://localhost"}, nil)
	require.NoError(t, err)
	u := up.(*uploader)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u.ctx = ctx
	u.jobs = cluster.FairQueue(ctx, u.queue, cluster.FairQueueOpts{Workers: 2})
	for n := 0; n < 2; n++ {
		u.wg.Add(1)
		go u.worker(n)
	}
	makeBatch := func(table string) *cluster.Batch {
		seg, err := wal.NewSegment(t.TempDir(), "DB_"+table+"_s")
		require.NoError(t, err)
		data := schema.AppendCSVHeader(nil, schema.DefaultMetricsMapping)
		data = append(data, fmt.Sprintf("%s,0,\"{}\",1\n", time.Now().UTC().Format(time.RFC3339Nano))...)
		_, err = seg.Write(ctx, data)
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		b := &cluster.Batch{Database: "DB", Table: table, Segments: []wal.SegmentInfo{seg.Info()}}
		setBatcher(t, b, &stubBatcher{})
		return b
	}
	a := makeBatch("A")
	u.queue <- a
	select {
	case table := <-conn.started:
		require.Equal(t, "A", table)
	case <-time.After(time.Second):
		t.Fatal("A did not start")
	}
	u.queue <- makeBatch("A")
	b := makeBatch("B")
	u.queue <- b
	select {
	case table := <-conn.started:
		require.Equal(t, "B", table)
	case <-time.After(time.Second):
		t.Fatal("B starved")
	}
	require.Eventually(t, b.IsReleased, time.Second, time.Millisecond)
	require.False(t, b.IsRemoved())
	cancel()
	u.wg.Wait()
	require.True(t, a.IsReleased())
}
