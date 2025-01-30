package limiter

import (
	"net"
	"strings"
	"testing"

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
