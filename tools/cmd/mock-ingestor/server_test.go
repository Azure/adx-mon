package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type policyFunc func(metadata) decision

func (f policyFunc) decide(meta metadata) decision { return f(meta) }

func fixedPolicy(o outcome, delay time.Duration) policy {
	return policyFunc(func(metadata) decision { return decision{o, delay} })
}

func testMock(t *testing.T, c config, p policy) *mockIngestor {
	t.Helper()
	m := newMockIngestor(c, p, log.New(io.Discard, "", 0))
	t.Cleanup(func() { m.stop(); m.handlers.Wait() })
	return m
}

func testClient(t *testing.T, gzip bool) *cluster.Client {
	t.Helper()
	c, err := cluster.NewClient(cluster.ClientOpts{DisableGzip: !gzip, DisableHTTP2: true, DisableKeepAlives: true, Timeout: 3 * time.Second, ResponseHeaderTimeout: 2 * time.Second})
	require.NoError(t, err)
	return c
}

func TestClientAcceptPlainAndGzip(t *testing.T) {
	for _, gzip := range []bool{false, true} {
		t.Run(fmt.Sprint(gzip), func(t *testing.T) {
			meta := make(chan metadata, 1)
			m := testMock(t, testConfig(t), policyFunc(func(md metadata) decision {
				meta <- md
				return decision{outcome: accept}
			}))
			s := httptest.NewServer(m)
			defer s.Close()
			const size = 8 << 20
			err := testClient(t, gzip).Write(context.Background(), s.URL, "../../DB_Table_schema_123.wal", io.LimitReader(repeatingReader{}, size))
			require.NoError(t, err)
			md := <-meta
			require.Equal(t, "../../DB_Table_schema_123.wal", md.filename)
			require.NotEmpty(t, md.requestID)
			m.handlers.Wait()
			x := m.stats.snapshot()
			require.EqualValues(t, 1, x.attempts[accept])
			require.EqualValues(t, 1, x.responses[202])
			require.Positive(t, x.acceptedBytes)
			require.Equal(t, x.receivedBytes, x.acceptedBytes)
			if gzip {
				require.Less(t, x.acceptedBytes, uint64(size))
			} else {
				require.EqualValues(t, size, x.acceptedBytes)
			}
		})
	}
}

type repeatingReader struct{}

func (repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestClientOverload(t *testing.T) {
	for _, tc := range []struct {
		scope string
		retry time.Duration
		want  time.Duration
	}{{"instance", 5 * time.Second, 0}, {"table", time.Second + time.Nanosecond, 2 * time.Second}, {"table", 301 * time.Second, 300 * time.Second}} {
		t.Run(fmt.Sprint(tc.scope, tc.retry), func(t *testing.T) {
			c := testConfig(t)
			c.overloadScope, c.retryAfter = tc.scope, tc.retry
			m := testMock(t, c, fixedPolicy(reject, 0))
			s := httptest.NewServer(m)
			defer s.Close()
			err := testClient(t, true).Write(context.Background(), s.URL, "DB_Table_schema_123.wal", strings.NewReader("payload"))
			if tc.scope == "instance" {
				require.ErrorIs(t, err, cluster.ErrPeerOverloaded)
			} else {
				var overload *wal.TableOverloadedError
				require.ErrorAs(t, err, &overload)
				require.Equal(t, wal.Destination{Database: "DB", Table: "Table"}, overload.Destination)
				require.Equal(t, overloadReason, overload.Reason)
				require.Equal(t, tc.want, overload.RetryAfter)
			}
			m.handlers.Wait()
			x := m.stats.snapshot()
			require.EqualValues(t, 1, x.responses[429])
			require.Positive(t, x.receivedBytes)
			require.Zero(t, x.acceptedBytes)
			require.Zero(t, x.disconnects)
		})
	}
}

// Noncompressible, unbounded input forces the streaming gzip compressor to
// notice an interrupted pipe instead of hiding the disconnect in a tiny body.
type endlessReader struct {
	state uint64
	reads atomic.Int64
}

func (r *endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		r.state ^= r.state << 13
		r.state ^= r.state >> 7
		r.state ^= r.state << 17
		p[i] = byte(r.state)
	}
	r.reads.Add(1)
	return len(p), nil
}

func TestClientDisconnectStreaming(t *testing.T) {
	for _, gzip := range []bool{false, true} {
		t.Run(fmt.Sprint(gzip), func(t *testing.T) {
			m := testMock(t, testConfig(t), fixedPolicy(disconnect, time.Hour))
			s := httptest.NewServer(m)
			defer s.Close()
			for range 10 {
				reader := &endlessReader{state: 42}
				err := testClient(t, gzip).Write(context.Background(), s.URL, "DB_Table_schema_123.wal", reader)
				require.Error(t, err)
				require.NotErrorIs(t, err, cluster.ErrPeerOverloaded)
				var transport *net.OpError
				// net/http may surface EOF, ECONNRESET, or a socket write failure.
				require.True(t, errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &transport), "%v", err)
				// Verify the failed request stops consuming its source. TestMain
				// also verifies compressor/transport goroutines do not leak.
				require.Eventually(t, func() bool {
					before := reader.reads.Load()
					time.Sleep(5 * time.Millisecond)
					return reader.reads.Load() == before
				}, time.Second, 10*time.Millisecond)
			}
			m.handlers.Wait()
			x := m.stats.snapshot()
			require.EqualValues(t, 10, x.attempts[disconnect])
			require.EqualValues(t, 10, x.disconnects)
			require.Empty(t, x.responses)
			require.Zero(t, x.receivedBytes)
			require.Zero(t, x.acceptedBytes)
		})
	}
}

type boundedReader struct {
	remaining int
	maxRead   int
}

func (r *boundedReader) Read(p []byte) (int, error) {
	r.maxRead = max(r.maxRead, len(p))
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	r.remaining -= n
	return n, nil
}

func TestStreamingOpaqueBodiesAndOverloadHeaders(t *testing.T) {
	for _, o := range []outcome{accept, reject} {
		for _, scope := range []string{"table", "instance"} {
			t.Run(string(o)+"/"+scope, func(t *testing.T) {
				c := testConfig(t)
				c.overloadScope, c.retryAfter = scope, time.Nanosecond
				m := testMock(t, c, fixedPolicy(o, 0))
				body := &boundedReader{remaining: 32 << 20}
				r := httptest.NewRequest(http.MethodPost, "/transfer?filename=%2Ftmp%2Fnot-a-local-file", body)
				r.Header.Set("Content-Encoding", "gzip") // Deliberately not valid gzip.
				w := httptest.NewRecorder()
				m.ServeHTTP(w, r)
				require.Zero(t, body.remaining)
				require.LessOrEqual(t, body.maxRead, 32*1024)
				require.EqualValues(t, 32<<20, m.stats.snapshot().receivedBytes)
				if o == accept {
					require.Equal(t, 202, w.Code)
					require.Empty(t, w.Header().Get(overloadScopeHeader))
				} else {
					require.Equal(t, 429, w.Code)
					require.Equal(t, scope, w.Header().Get(overloadScopeHeader))
					require.Equal(t, "1", w.Header().Get("Retry-After"))
					if scope == "table" {
						require.Equal(t, overloadReason, w.Header().Get(overloadReasonHeader))
						require.Empty(t, w.Header().Get("Connection"))
					} else {
						require.Equal(t, "close", w.Header().Get("Connection"))
						require.Empty(t, w.Header().Get(overloadReasonHeader))
					}
				}
			})
		}
	}
}

type eofTimeReader struct {
	reader io.Reader
	eofAt  time.Time
}

func (r *eofTimeReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		r.eofAt = time.Now()
	}
	return n, err
}

func TestResponseDelayStartsAfterBodyConsumption(t *testing.T) {
	for _, o := range []outcome{accept, reject} {
		c := testConfig(t)
		c.verbose = true
		m := testMock(t, c, fixedPolicy(o, 10*time.Millisecond))
		var logs bytes.Buffer
		m.logger = log.New(&logs, "", 0)
		body := &eofTimeReader{reader: strings.NewReader("payload")}
		r := httptest.NewRequest("POST", "/transfer?filename=metadata-only.wal", body)
		r.Header.Set("X-Request-ID", "test-request")
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		require.False(t, body.eofAt.IsZero())
		require.GreaterOrEqual(t, time.Since(body.eofAt), 10*time.Millisecond)
		require.EqualValues(t, 7, m.stats.snapshot().receivedBytes)
		require.Contains(t, logs.String(), "filename=\"metadata-only.wal\"")
		require.Contains(t, logs.String(), "request_id=\"test-request\"")
		if o == accept {
			require.Equal(t, 202, w.Code)
		} else {
			require.Equal(t, 429, w.Code)
		}
	}
}

func TestRunHelpValidationListenFailureAndCanceledLifecycle(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"-h"}, &output))
	require.Contains(t, output.String(), "accept-rate")
	require.Error(t, run(context.Background(), []string{"-accept-rate", "2"}, io.Discard))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	require.ErrorContains(t, run(context.Background(), []string{"-listen-addr", listener.Addr().String()}, io.Discard), "listen:")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output.Reset()
	require.NoError(t, run(ctx, []string{"-listen-addr", "127.0.0.1:0", "-seed", "42"}, &output))
	require.Contains(t, output.String(), "seed=42")
	require.Contains(t, output.String(), "stats=final")
}

func TestMalformedAndProbeRequestsDoNotInvokePolicy(t *testing.T) {
	m := testMock(t, testConfig(t), policyFunc(func(metadata) decision { t.Error("policy invoked"); return decision{outcome: disconnect} }))
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/transfer?filename=x", 405}, {"POST", "/transfer", 400}, {"POST", "/transfer?filename=", 400},
		{"POST", "/transfer?filename=%GG", 400}, {"POST", "/transfer?filename=x&other=%GG", 400},
		{"GET", "/readyz", 200}, {"GET", "/metrics", 200}, {"POST", "/elsewhere", 404},
	} {
		w := httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, tc.status, w.Code, tc.path)
	}
	x := m.stats.snapshot()
	require.Zero(t, x.attempts[accept]+x.attempts[reject]+x.attempts[disconnect])
	m.stop()
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	require.Equal(t, 503, w.Code)
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("POST", "/transfer?filename=x", nil))
	require.Equal(t, 503, w.Code)
}

type partialBody struct{ read bool }

func (b *partialBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.ErrUnexpectedEOF
	}
	b.read = true
	return copy(p, "partial"), nil
}
func (*partialBody) Close() error { return nil }

func TestIncompleteUpload(t *testing.T) {
	for _, o := range []outcome{accept, reject} {
		m := testMock(t, testConfig(t), fixedPolicy(o, 0))
		r := httptest.NewRequest("POST", "/transfer?filename=x", &partialBody{})
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
		x := m.stats.snapshot()
		require.EqualValues(t, 1, x.readErrors)
		require.EqualValues(t, len("partial"), x.receivedBytes)
		require.Zero(t, x.acceptedBytes)
		require.Zero(t, x.responses[202])
		require.Zero(t, x.responses[429])
	}
}

func TestIncompleteUploadOnSocket(t *testing.T) {
	m := testMock(t, testConfig(t), fixedPolicy(accept, 0))
	s := httptest.NewServer(m)
	defer s.Close()
	conn, err := net.Dial("tcp", s.Listener.Addr().String())
	require.NoError(t, err)
	_, err = io.WriteString(conn, "POST /transfer?filename=x HTTP/1.1\r\nHost: local\r\nContent-Length: 100000\r\n\r\npartial")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return m.stats.snapshot().receivedBytes > 0 }, time.Second, time.Millisecond)
	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return m.stats.snapshot().finished == 1 }, time.Second, time.Millisecond)
	x := m.stats.snapshot()
	require.EqualValues(t, 1, x.readErrors)
	require.Zero(t, x.acceptedBytes)
	require.Zero(t, x.responses[202])
}

type blockingBody struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func TestCancellationInterruptsBodyReadAndDelay(t *testing.T) {
	for _, phase := range []string{"body", "delay"} {
		for _, shutdown := range []bool{false, true} {
			t.Run(fmt.Sprint(phase, "/shutdown=", shutdown), func(t *testing.T) {
				m := testMock(t, testConfig(t), fixedPolicy(accept, time.Hour))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var body io.Reader = strings.NewReader("uploaded")
				blocked := &blockingBody{started: make(chan struct{}), closed: make(chan struct{})}
				if phase == "body" {
					body = blocked
				}
				r := httptest.NewRequest("POST", "/transfer?filename=x", body).WithContext(ctx)
				w := httptest.NewRecorder()
				done := make(chan struct{})
				go func() { defer close(done); m.ServeHTTP(w, r) }()
				if phase == "body" {
					<-blocked.started
				} else {
					require.Eventually(t, func() bool { return m.stats.snapshot().receivedBytes == 8 }, time.Second, time.Millisecond)
				}
				if shutdown {
					m.stop()
				} else {
					cancel()
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("cancellation did not interrupt handler")
				}
				x := m.stats.snapshot()
				require.EqualValues(t, 1, x.cancellations)
				if !shutdown {
					require.EqualValues(t, 1, x.clientCancellations)
				}
				require.Zero(t, x.acceptedBytes)
				require.Zero(t, x.responses[202])
				require.Zero(t, x.inflight)
			})
		}
	}
}

func TestClientCancellationDuringResponseDelay(t *testing.T) {
	m := testMock(t, testConfig(t), fixedPolicy(accept, time.Hour))
	s := httptest.NewServer(m)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	c := testClient(t, true)
	go func() { done <- c.Write(ctx, s.URL, "x", strings.NewReader("uploaded")) }()
	require.Eventually(t, func() bool { return m.stats.snapshot().receivedBytes > 0 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Eventually(t, func() bool { return m.stats.snapshot().finished == 1 }, time.Second, time.Millisecond)
	x := m.stats.snapshot()
	require.EqualValues(t, 1, x.clientCancellations)
	require.Zero(t, x.acceptedBytes)
	require.Zero(t, x.responses[202])
}

func TestClientCancellationDuringStreamingUpload(t *testing.T) {
	for _, gzip := range []bool{false, true} {
		t.Run(fmt.Sprint(gzip), func(t *testing.T) {
			m := testMock(t, testConfig(t), fixedPolicy(accept, 0))
			s := httptest.NewServer(m)
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			c := testClient(t, gzip)
			reader := &endlessReader{state: 42}
			go func() { done <- c.Write(ctx, s.URL, "x", reader) }()
			require.Eventually(t, func() bool { return m.stats.snapshot().receivedBytes > 0 }, time.Second, time.Millisecond)
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("client upload did not stop")
			}
			require.Eventually(t, func() bool { return m.stats.snapshot().finished == 1 }, time.Second, time.Millisecond)
			x := m.stats.snapshot()
			require.Zero(t, x.acceptedBytes)
			require.Zero(t, x.responses[202])
			require.EqualValues(t, 1, x.readErrors)
		})
	}
}

type untouchedBody struct{}

func (untouchedBody) Read([]byte) (int, error) { panic("disconnect read body") }
func (untouchedBody) Close() error             { panic("disconnect closed/drained body") }

type hijackWriter struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (w hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, nil, nil
}

func TestDisconnectDoesNotReadCloseBodyOrWriteResponse(t *testing.T) {
	m := testMock(t, testConfig(t), fixedPolicy(disconnect, time.Hour))
	server, client := net.Pipe()
	defer client.Close()
	w := hijackWriter{httptest.NewRecorder(), server}
	m.ServeHTTP(w, httptest.NewRequest("POST", "/transfer?filename=x", untouchedBody{}))
	_, err := client.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.EOF)
	require.Empty(t, w.Header())
	require.Empty(t, w.Body.String())
	require.False(t, w.Flushed)
	x := m.stats.snapshot()
	require.EqualValues(t, 1, x.disconnects)
	require.Empty(t, x.responses)
	require.Empty(t, m.hijacked)
}

type failedFlushWriter struct{ *httptest.ResponseRecorder }

func (failedFlushWriter) FlushError() error { return io.ErrClosedPipe }

func TestResponseWriteFailureIsNotCountedAccepted(t *testing.T) {
	m := testMock(t, testConfig(t), fixedPolicy(accept, 0))
	w := failedFlushWriter{httptest.NewRecorder()}
	m.ServeHTTP(w, httptest.NewRequest("POST", "/transfer?filename=x", strings.NewReader("payload")))
	x := m.stats.snapshot()
	require.EqualValues(t, 1, x.attempts[accept])
	require.EqualValues(t, 7, x.receivedBytes)
	require.EqualValues(t, 1, x.writeErrors)
	require.Zero(t, x.acceptedBytes)
	require.Empty(t, x.responses)
}

func TestUnsupportedHijackAndInvalidPolicyDoNotAcknowledge(t *testing.T) {
	for _, d := range []decision{{outcome: disconnect}, {outcome: "unknown"}, {outcome: accept, delay: -1}} {
		m := testMock(t, testConfig(t), policyFunc(func(metadata) decision { return d }))
		w := httptest.NewRecorder()
		m.ServeHTTP(w, httptest.NewRequest("POST", "/transfer?filename=x", nil))
		require.Equal(t, 500, w.Code)
		require.Zero(t, m.stats.snapshot().acceptedBytes)
		require.Zero(t, m.stats.snapshot().disconnects)
	}
	m := testMock(t, testConfig(t), fixedPolicy(accept, 0))
	r := httptest.NewRequest("POST", "/transfer?filename=x", nil)
	r.ProtoMajor = 2
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	require.Equal(t, 505, w.Code)
	require.Zero(t, m.stats.snapshot().attempts[accept])
}

func TestShutdownClosesTrackedAndLateHijacks(t *testing.T) {
	m := testMock(t, testConfig(t), fixedPolicy(disconnect, 0))
	for _, late := range []bool{false, true} {
		server, client := net.Pipe()
		if late {
			m.stop()
		}
		m.trackHijacked(server)
		m.stop()
		_, err := client.Read(make([]byte, 1))
		require.ErrorIs(t, err, io.EOF)
		require.NoError(t, client.Close())
	}
	require.Empty(t, m.hijacked)
}

func TestLifecycleShutdownInterruptsSocketReads(t *testing.T) {
	c := testConfig(t)
	c.responseDelay, c.statsInterval = time.Hour, 5*time.Millisecond
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, listener, c, log.New(&output, "", 0)) }()
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "POST /transfer?filename=x HTTP/1.1\r\nHost: local\r\nContent-Length: 100000\r\n\r\npartial")
	require.NoError(t, err)
	// An independent ready probe confirms the actual lifecycle listener is serving.
	probe, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	_, err = io.WriteString(probe, "GET /readyz HTTP/1.1\r\nHost: local\r\nConnection: close\r\n\r\n")
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(probe), nil)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()
	_ = probe.Close()
	time.Sleep(20 * time.Millisecond) // Exercise at least one periodic summary.
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not interrupt active body read")
	}
	require.Contains(t, output.String(), "WAL DELETION")
	require.Contains(t, output.String(), "stats=periodic")
	require.Contains(t, output.String(), "stats=final")
	require.Contains(t, output.String(), "inflight=0")
}

func TestConcurrentTrafficAndMetrics(t *testing.T) {
	c := testConfig(t)
	c.acceptRate, c.disconnectRate, c.seed, c.overloadScope = .6, .2, 42, "table"
	m := testMock(t, c, newRandomPolicy(c))
	s := httptest.NewServer(m)
	defer s.Close()
	client := testClient(t, true)
	var wg sync.WaitGroup
	const workers, attempts = 8, 20
	for range workers {
		wg.Go(func() {
			for range attempts {
				_ = client.Write(context.Background(), s.URL, "DB_Table_schema_123.wal", strings.NewReader("payload"))
			}
		})
	}
	for range 10 {
		for _, path := range []string{"/readyz", "/metrics"} {
			resp, err := s.Client().Get(s.URL + path)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}
	wg.Wait()
	m.handlers.Wait()
	x := m.stats.snapshot()
	require.EqualValues(t, workers*attempts, x.finished)
	require.EqualValues(t, workers*attempts, x.attempts[accept]+x.attempts[reject]+x.attempts[disconnect])
	require.Equal(t, x.attempts[accept], x.responses[202])
	require.Equal(t, x.attempts[reject], x.responses[429])
	require.Equal(t, x.attempts[disconnect], x.disconnects)
	require.Zero(t, x.inflight)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	metrics := w.Body.String()
	for _, name := range []string{"mock_ingestor_attempts_total{outcome=\"accept\"}", "mock_ingestor_responses_total{status=\"202\"}", "mock_ingestor_responses_total{status=\"429\"}", "mock_ingestor_accepted_body_bytes_total", "mock_ingestor_transfer_duration_seconds_count", "mock_ingestor_received_body_bytes_per_second"} {
		require.Contains(t, metrics, name)
	}
	require.NotContains(t, metrics, "filename=")
	require.NotContains(t, metrics, "request_id=")
}
