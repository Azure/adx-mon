package ingestor

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A sender that stops sending its body mid-transfer releases the handler, and the intake memory it holds, after the
// stall timeout instead of holding it indefinitely.
func TestHandleTransferReleasesStalledSender(t *testing.T) {
	defer func(d time.Duration) { transferStallTimeout = d }(transferStallTimeout)
	transferStallTimeout = 200 * time.Millisecond

	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store: &fakeStore{importFn: func(_ string, body io.ReadCloser) (int, error) {
			n, err := io.Copy(io.Discard, body)
			return int(n), err
		}},
		databases: map[string]struct{}{"testdb": {}},
	}
	srv := httptest.NewServer(http.HandlerFunc(s.HandleTransfer))
	defer srv.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	defer conn.Close()
	// Send the headers and the first chunk of the body, then stall.
	_, err = fmt.Fprintf(conn, "POST /transfer?filename=testdb_Table_2f2c74ffa19e6000.wal HTTP/1.1\r\nHost: ingestor\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n")
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Logf("handler still waiting on the stalled sender after %s: %v", time.Since(start).Round(100*time.Millisecond), err)
	}
	require.NoError(t, err, "handler was not released")
	resp.Body.Close()
	t.Logf("stalled sender released after %s with %s", time.Since(start).Round(100*time.Millisecond), resp.Status)
	require.Less(t, time.Since(start), 2*time.Second)
}
