package clickhouse

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	chproto "github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
)

type releasedBatchConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *releasedBatchConn) Write(p []byte) (int, error)     { return len(p), nil }
func (c *releasedBatchConn) SetReadDeadline(time.Time) error { return nil }
func (c *releasedBatchConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestNativeBatchStopsWatchdogBeforeReleasingTransport(t *testing.T) {
	for _, operation := range []string{"Close", "Flush"} {
		t.Run(operation, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				transport := &releasedBatchConn{closed: make(chan struct{})}
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
					ctx:          ctx,
					conn:         &connect{conn: transport, logger: slog.Default(), buffer: new(chproto.Buffer), reader: chproto.NewReader(bytes.NewReader([]byte{proto.ServerEndOfStream})), revision: ClientTCPProtocolVersion},
					block:        block,
					closeOnFlush: true,
					connRelease: func(_ *connect, err error) {
						if err != nil {
							t.Fatalf("unexpected release error: %v", err)
						}
						// The transport is now available to another borrower. Its old
						// batch must have stopped its watchdog before this hand-off.
						cancel()
						synctest.Wait()
						select {
						case <-transport.closed:
							t.Error("old batch cancellation closed a transport after successful pool release")
						default:
						}
					},
				}
				var err error
				if operation == "Close" {
					err = b.Close()
				} else {
					err = b.Flush()
				}
				if err != nil {
					t.Fatal(err)
				}
				if !b.released {
					t.Fatal("transport was not released")
				}
			})
		})
	}
}

func TestNativeBatchWatchdogStopJoinsRunningCallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered, finish, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
		stop := contextWatchdog(ctx, func() { close(entered); <-finish })
		cancel()
		<-entered
		go func() { stop(); close(stopped) }()
		synctest.Wait()
		select {
		case <-stopped:
			t.Error("watchdog stop returned before the transport-close callback finished")
			close(finish)
			return
		default:
		}
		close(finish)
		<-stopped
		stop() // Safe when an explicit pre-release stop is followed by a defer.
	})
}

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
