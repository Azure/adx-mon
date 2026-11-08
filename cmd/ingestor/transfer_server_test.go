package main

import (
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// transferWithHeader sends a request with a header of size bytes to the transfer server and returns its status.
func transferWithHeader(t *testing.T, size int) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := newTransferServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }))
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	req, err := http.NewRequest("POST", "http://"+ln.Addr().String()+"/transfer", strings.NewReader("x"))
	require.NoError(t, err)
	req.Header.Set("X-Pad", strings.Repeat("a", size))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

// The transfer server accepts headers up to 64 KB, which bounds what each connection can hold before its request is
// admitted.
func TestTransferServerHeaderLimit(t *testing.T) {
	require.Equal(t, 64<<10, transferMaxHeaderBytes)
	require.Equal(t, http.StatusAccepted, transferWithHeader(t, 32<<10))
	require.Equal(t, http.StatusRequestHeaderFieldsTooLarge, transferWithHeader(t, 128<<10))
}
