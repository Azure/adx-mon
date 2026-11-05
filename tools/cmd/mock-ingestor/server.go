package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	overloadScopeHeader  = "X-Adxmon-Overload-Scope"
	overloadReasonHeader = "X-Adxmon-Overload-Reason"
	overloadReason       = "mock-ingestor-policy"
)

type mockIngestor struct {
	config config
	policy policy
	stats  *statistics
	logger *log.Logger
	ctx    context.Context
	cancel context.CancelFunc
	mux    *http.ServeMux

	mu       sync.Mutex
	stopping bool
	hijacked map[net.Conn]struct{}
	handlers sync.WaitGroup
}

func newMockIngestor(c config, p policy, logger *log.Logger) *mockIngestor {
	ctx, cancel := context.WithCancel(context.Background())
	m := &mockIngestor{config: c, policy: p, stats: newStatistics(), logger: logger, ctx: ctx, cancel: cancel, hijacked: make(map[net.Conn]struct{})}
	registry := prometheus.NewRegistry()
	registry.MustRegister(m.stats)
	m.mux = http.NewServeMux()
	m.mux.HandleFunc("/transfer", m.transfer)
	m.mux.HandleFunc("/readyz", m.ready)
	m.mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	return m
}

func (m *mockIngestor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mux.ServeHTTP(w, r)
}

func (m *mockIngestor) ready(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	stopping := m.stopping
	m.mu.Unlock()
	if stopping {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ready\n")
}

// stop closes the registration gate before canceling handlers and closing all
// hijacked sockets. net/http.Server.Shutdown does not own hijacked sockets.
func (m *mockIngestor) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopping = true
	m.cancel()
	for conn := range m.hijacked {
		_ = conn.Close()
		delete(m.hijacked, conn)
	}
}

func (m *mockIngestor) trackHijacked(conn net.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopping {
		_ = conn.Close()
		return
	}
	m.hijacked[conn] = struct{}{}
}

func (m *mockIngestor) closeHijacked(conn net.Conn) {
	_ = conn.Close()
	m.mu.Lock()
	delete(m.hijacked, conn)
	m.mu.Unlock()
}

func (m *mockIngestor) transfer(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	m.handlers.Add(1)
	m.mu.Unlock()
	defer m.handlers.Done()
	started := time.Now()
	m.stats.update(func(s *snapshot) { s.inflight++ })
	ctx, cancel := context.WithCancel(r.Context())
	stopShutdown := context.AfterFunc(m.ctx, cancel)
	defer func() {
		stopShutdown()
		duration := time.Since(started).Seconds()
		m.stats.update(func(s *snapshot) {
			s.inflight--
			s.finished++
			s.durationSeconds += duration
			s.maxDurationSeconds = max(s.maxDurationSeconds, duration)
			if ctx.Err() != nil {
				s.cancellations++
			}
			if r.Context().Err() != nil {
				s.clientCancellations++
			}
		})
		cancel()
		m.stats.duration.Observe(duration)
	}()
	if r.ProtoMajor != 1 {
		m.respond(w, http.StatusHTTPVersionNotSupported, 0)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.Header().Set("Connection", "close")
		m.respond(w, http.StatusMethodNotAllowed, 0)
		return
	}
	// URL.Query silently ignores malformed escaping; ParseQuery must reject it.
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || query.Get("filename") == "" {
		w.Header().Set("Connection", "close")
		m.respond(w, http.StatusBadRequest, 0)
		return
	}
	meta := metadata{filename: query.Get("filename"), requestID: r.Header.Get("X-Request-ID")}
	d := m.policy.decide(meta)
	if d.outcome != accept && d.outcome != reject && d.outcome != disconnect || d.delay < 0 {
		// A broken injected policy must never accidentally acknowledge data.
		w.Header().Set("Connection", "close")
		m.respond(w, http.StatusInternalServerError, 0)
		return
	}
	m.stats.update(func(s *snapshot) { s.attempts[d.outcome]++ })
	if m.config.verbose {
		m.logger.Printf("transfer filename=%q request_id=%q selected=%s delay=%s", meta.filename, meta.requestID, d.outcome, d.delay)
	}
	if d.outcome == disconnect {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			m.stats.update(func(s *snapshot) { s.writeErrors++ })
			w.Header().Set("Connection", "close")
			m.respond(w, http.StatusInternalServerError, 0)
			return
		}
		m.trackHijacked(conn)
		m.closeHijacked(conn)
		m.stats.update(func(s *snapshot) { s.disconnects++ })
		// Do not defer Body.Close or drain: this outcome interrupts the upload.
		return
	}

	// Context cancellation alone does not interrupt a blocking server body Read.
	// Set a socket deadline first: net/http Body.Close can wait on that same Read.
	readStopped := make(chan struct{})
	stopRead := context.AfterFunc(ctx, func() {
		defer close(readStopped)
		if err := http.NewResponseController(w).SetReadDeadline(time.Now()); err != nil {
			_ = r.Body.Close() // Injectable/direct-test bodies must unblock on Close.
		}
	})
	defer func() {
		if !stopRead() {
			<-readStopped // ResponseController may not be used after ServeHTTP.
		}
	}()
	// Hide optional WriterTo optimizations so every upload uses a fixed buffer.
	n, readErr := io.CopyBuffer(bodyCounter{m.stats}, struct{ io.Reader }{r.Body}, make([]byte, 32*1024))
	if readErr != nil {
		m.stats.update(func(s *snapshot) { s.readErrors++ })
		if ctx.Err() == nil {
			w.Header().Set("Connection", "close")
			m.respond(w, http.StatusBadRequest, 0)
		}
		return
	}
	if d.delay > 0 {
		timer := time.NewTimer(d.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
	if ctx.Err() != nil || m.ctx.Err() != nil {
		return
	}
	if d.outcome == reject {
		w.Header().Set(overloadScopeHeader, m.config.overloadScope)
		w.Header().Set("Retry-After", m.config.retrySeconds())
		if m.config.overloadScope == "table" {
			w.Header().Set(overloadReasonHeader, overloadReason)
		} else {
			w.Header().Set("Connection", "close")
		}
		m.respond(w, http.StatusTooManyRequests, 0)
		return
	}
	m.respond(w, http.StatusAccepted, uint64(n))
}

type bodyCounter struct{ stats *statistics }

func (b bodyCounter) Write(p []byte) (int, error) {
	b.stats.update(func(s *snapshot) { s.receivedBytes += uint64(len(p)) })
	return len(p), nil
}

func (m *mockIngestor) respond(w http.ResponseWriter, status int, acceptedBytes uint64) {
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(status)
	if err := http.NewResponseController(w).Flush(); err != nil {
		m.stats.update(func(s *snapshot) { s.writeErrors++ })
		return
	}
	m.stats.update(func(s *snapshot) {
		s.responses[status]++
		s.acceptedBytes += acceptedBytes
	})
}
