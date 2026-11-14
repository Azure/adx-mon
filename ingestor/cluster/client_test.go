package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestClient_Write_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/transfer", r.URL.Path)
		require.Equal(t, "filename=testfile", r.URL.RawQuery)
		require.Equal(t, "text/csv", r.Header.Get("Content-Type"))
		require.Equal(t, "adx-mon", r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.NoError(t, err)
}

// TestClient_Write_Gzip_SequentialReuse verifies that successive Client.Write
// calls produce the correct decompressed payload. Catches state bleed across
// pooled *gzip.Writer instances (e.g. forgetting to Reset).
func TestClient_Write_Gzip_SequentialReuse(t *testing.T) {
	var got [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		gzipReader, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer gzipReader.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, gzipReader)
		require.NoError(t, err)
		got = append(got, append([]byte(nil), buf.Bytes()...))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{Timeout: 5 * time.Second, DisableGzip: false})
	require.NoError(t, err)

	payloads := [][]byte{
		[]byte("first payload"),
		bytes.Repeat([]byte("ABCDEFGH"), 1024),                      // 8 KiB, highly compressible
		bytes.Repeat([]byte("xyz"), 50000),                          // ~150 KiB, spans many flate blocks
		[]byte("short"),                                             // tiny again, writer state must be clean
		bytes.Repeat([]byte("payload-five-distinct-content"), 8000), // ~232 KiB
	}

	for i, p := range payloads {
		err := client.Write(context.Background(), server.URL, "testfile", bytes.NewReader(p))
		require.NoError(t, err, "write %d", i)
	}

	require.Equal(t, len(payloads), len(got))
	for i, p := range payloads {
		require.Equal(t, p, got[i], "payload %d mismatch", i)
	}
}

// TestClient_Write_Gzip_ConcurrentReuse exercises the gzip pool from many
// goroutines simultaneously. With -race this catches accidental sharing of a
// pooled *gzip.Writer across goroutines.
func TestClient_Write_Gzip_ConcurrentReuse(t *testing.T) {
	var (
		mu  sync.Mutex
		got = map[string]int{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gzipReader, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer gzipReader.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, gzipReader)
		require.NoError(t, err)
		mu.Lock()
		got[buf.String()]++
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:             5 * time.Second,
		DisableGzip:         false,
		MaxConnsPerHost:     32,
		MaxIdleConnsPerHost: 32,
	})
	require.NoError(t, err)

	const workers, perWorker = 16, 25
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(w int) {
			defer wg.Done()
			payload := bytes.Repeat(fmt.Appendf(nil, "worker-%02d-data ", w), 2048) // ~32 KiB each, distinct per worker
			for range perWorker {
				err := client.Write(context.Background(), server.URL, "testfile", bytes.NewReader(payload))
				require.NoError(t, err)
			}
		}(w)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, got, workers, "each worker's payload should have been received intact and distinct")
	for k, count := range got {
		require.Equal(t, perWorker, count, "payload %q received %d times, expected %d", k[:32], count, perWorker)
	}
}

func BenchmarkClientWrite_GzipReuse(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:     30 * time.Second,
		DisableGzip: false,
	})
	require.NoError(b, err)

	// Representative payload size for a transfer batch.
	payload := bytes.Repeat([]byte("metric,value,timestamp\n"), 4096)

	b.ReportAllocs()
	for b.Loop() {
		if err := client.Write(context.Background(), server.URL, "testfile", bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func TestClient_Write_BadRequest(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request error"))
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.IsType(t, &ErrBadRequest{}, err)
	require.Contains(t, err.Error(), "bad request error")
	require.True(t, errors.Is(err, &ErrBadRequest{}))
}

func TestClient_Write_TooManyRequests(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPeerOverloaded))
}

func TestClient_Write_SegmentExists(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSegmentExists))
}

func TestClient_Write_HTTPError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "internal server error")
}

func TestClient_Write_DisableGzip_False(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/transfer", r.URL.Path)
		require.Equal(t, "filename=testfile", r.URL.RawQuery)
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding")) // Ensure gzip encoding is set

		// Decompress gzip data
		gzipReader, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer gzipReader.Close()

		var decompressed bytes.Buffer
		_, err = io.Copy(&decompressed, gzipReader)
		require.NoError(t, err)
		require.Equal(t, "testdata", decompressed.String()) // Ensure data is compressed and matches
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:     time.Second,
		DisableGzip: false,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.NoError(t, err)
}

func TestClient_Write_GzipGoroutineExitsWhenRequestFails(t *testing.T) {
	// Reserve a port and close the listener so every dial is refused and
	// httpClient.Do fails before the gzip pipe is drained.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := server.URL
	server.Close()

	client, err := NewClient(ClientOpts{Timeout: time.Second})
	require.NoError(t, err)

	defer goleak.VerifyNone(t)

	for range 5 {
		err = client.Write(context.Background(), endpoint, "testfile", strings.NewReader("testdata"))
		require.Error(t, err)
	}
}

// Only ingestors mark their transfers as peer transfers; collector transfers are unmarked.
func TestClientMarksPeerTransfers(t *testing.T) {
	for _, peer := range []bool{false, true} {
		var header string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header = r.Header.Get(PeerTransferHeader)
			w.WriteHeader(http.StatusAccepted)
		}))
		client, err := NewClient(ClientOpts{DisableGzip: true, PeerTransfer: peer})
		require.NoError(t, err)
		require.NoError(t, client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata")))
		server.Close()
		if peer {
			require.Equal(t, "true", header)
		} else {
			require.Empty(t, header)
		}
	}
}

// TestClient_Close_ReleasesIdleConnections demonstrates that without a
// Close method the keep-alive connection the client established to a peer
// persists on the server side until the transport's 90s IdleConnTimeout
// fires, leaking the connection and its TLS context / sync.Pool buffers.
// After the fix, Client.Close() calls CloseIdleConnections and the server
// observes the connection drop promptly.
func TestClient_Close_ReleasesIdleConnections(t *testing.T) {
	var openConns atomic.Int64

	// Wrap the listener to track how many connections are currently open.
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ln := &trackingListener{Listener: raw, open: &openConns}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	server.Listener = ln
	server.Start()
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
		IdleConnTimeout:    time.Hour, // long: the idle conn should NOT time out on its own during this test
	})
	require.NoError(t, err)

	// Establish one keep-alive connection to the peer.
	require.NoError(t, client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata")))
	require.EqualValues(t, 1, openConns.Load(), "one connection should be open after the request")

	// Close must release the idle keep-alive connection.
	require.NoError(t, client.Close())

	// The server should observe the connection close promptly.
	require.Eventually(t, func() bool {
		return openConns.Load() == 0
	}, 2*time.Second, 10*time.Millisecond,
		"idle connection was not released by Client.Close; it will leak until the transport's IdleConnTimeout fires")
}

// trackingListener counts open connections: it increments on each accepted
// connection and decrements when the underlying connection's Close is called
// by the http transport (which is exactly what happens when
// CloseIdleConnections runs, and when the server drops the connection).
type trackingListener struct {
	net.Listener
	open *atomic.Int64
}

func (l *trackingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.open.Add(1)
	return &trackingConn{Conn: c, open: l.open}, nil
}

type trackingConn struct {
	net.Conn
	open *atomic.Int64
	once sync.Once
}

func (c *trackingConn) Close() error {
	c.once.Do(func() { c.open.Add(-1) })
	return c.Conn.Close()
}

func TestClient_Write_SegmentLocked(t *testing.T) {
	var requestId string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestId = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusLocked)
	}))
	defer server.Close()

	client, err := NewClient(ClientOpts{
		Timeout:            time.Second,
		InsecureSkipVerify: true,
		DisableGzip:        true,
	})
	require.NoError(t, err)

	err = client.Write(context.Background(), server.URL, "testfile", strings.NewReader("testdata"))
	require.ErrorIs(t, err, ErrSegmentLocked)
	require.NotEmpty(t, requestId)
	require.EqualError(t, err, "segment is locked requestId="+requestId)
	require.EqualError(t, ErrSegmentLocked, "segment is locked", "the shared error is not changed")
}
