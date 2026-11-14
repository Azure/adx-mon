package clickhouse

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	chproto "github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
)

type stalledBatchConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *stalledBatchConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(p)
}

func TestNativeBatchFlushAndCloseCancelStalledWrite(t *testing.T) {
	for _, operation := range []string{"Flush", "Close"} {
		t.Run(operation, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			conn := &stalledBatchConn{Conn: client, entered: make(chan struct{})}
			defer conn.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			block := proto.NewBlock()
			if operation == "Flush" {
				if err := block.AddColumn("value", column.Type("String")); err != nil {
					t.Fatal(err)
				}
				if err := block.Append("value"); err != nil {
					t.Fatal(err)
				}
			}
			b := &batch{
				ctx:         ctx,
				conn:        &connect{conn: conn, logger: slog.Default(), buffer: new(chproto.Buffer), revision: ClientTCPProtocolVersion},
				block:       block,
				connRelease: func(_ *connect, _ error) { _ = conn.Close() },
			}
			result := make(chan error, 1)
			go func() {
				if operation == "Flush" {
					result <- b.Flush()
				} else {
					result <- b.Close()
				}
			}()
			select {
			case <-conn.entered:
			case <-time.After(time.Second):
				t.Fatal("batch never started its socket write")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context cancellation, got %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("stalled write did not stop on cancellation")
			}
		})
	}
}
