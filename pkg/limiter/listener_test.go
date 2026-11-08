package limiter

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestLimitListener_ExcessConnectionIsDropped(t *testing.T) {
	originalCount := getActiveConnections(t)

	// Create a simple listener that always returns the same connection
	n := &net.TCPConn{}
	t.Cleanup(func() { n.Close() })
	fakeL := &fakeConnListener{conn: n}

	// Limit to 1 connection
	ll := LimitListener(fakeL, 1)

	conn, err := ll.Accept()
	require.NoError(t, err)

	// Duplicate close should not cause our counter to drop below zero
	conn.Close()
	conn.Close()

	// Validate counter should return to original count
	require.Equal(t, getActiveConnections(t), originalCount)
}

func getActiveConnections(t *testing.T) float64 {
	t.Helper()

	mets, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	var activeConns float64
	for _, v := range mets {
		switch *v.Type {
		case io_prometheus_client.MetricType_GAUGE:
			for _, vv := range v.Metric {
				if strings.Contains(v.GetName(), "ingestor_active_connections") {
					activeConns += vv.Gauge.GetValue()
				}
			}
		}
	}

	return activeConns
}

type fakeConnListener struct {
	conn net.Conn
}

func (f *fakeConnListener) Accept() (net.Conn, error) {
	return f.conn, nil
}

func (f *fakeConnListener) Close() error   { return nil }
func (f *fakeConnListener) Addr() net.Addr { return nil }

// A connection beyond the limit is closed before anything is read from it, so its sender gets no response.
func TestLimitListener_OverLimitConnectionGetsNoResponse(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ll := LimitListener(inner, 1)
	t.Cleanup(func() { ll.Close() })
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })}
	go srv.Serve(ll)
	t.Cleanup(func() { srv.Close() })

	held, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	defer held.Close()
	_, err = held.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(held), nil)
	require.NoError(t, err)
	resp.Body.Close()

	dropped := counter(t, "ingestor_dropped_connections_total")
	over, err := net.Dial("tcp", inner.Addr().String())
	require.NoError(t, err)
	defer over.Close()
	_, _ = over.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, over.SetReadDeadline(time.Now().Add(5*time.Second)))
	n, err := over.Read(make([]byte, 1))
	require.Zero(t, n)
	require.Error(t, err)
	require.False(t, errors.Is(err, os.ErrDeadlineExceeded), "the connection is closed, not left open")
	require.Equal(t, 1.0, counter(t, "ingestor_dropped_connections_total")-dropped)
}

func counter(t *testing.T, name string) float64 {
	t.Helper()
	mets, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, v := range mets {
		if strings.HasSuffix(v.GetName(), name) {
			return v.Metric[0].GetCounter().GetValue()
		}
	}
	return 0
}
