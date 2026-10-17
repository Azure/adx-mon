package otlp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func rawlogRequest(t *testing.T) *http.Request {
	t.Helper()
	var req v1.ExportLogsServiceRequest
	require.NoError(t, protojson.Unmarshal(rawlog, &req))
	data, err := proto.Marshal(&req)
	require.NoError(t, err)
	r := httptest.NewRequest("POST", "/v1/logs", bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/x-protobuf")
	return r
}

// Close must wait for synchronous request processing, otherwise the owner tears
// down transforms and sinks while a request is still using them.
func TestCloseWaitsForInFlightRequests(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var finished bool

	s := NewLogsService(LogsServiceOpts{
		HealthChecker: fakeHealthChecker{healthy: true},
		ProcessBatch: func(ctx context.Context, batch *types.LogBatch) (int64, error) {
			close(entered)
			<-release
			finished = true
			types.DisposeLogBatch(batch)
			return 0, nil
		},
	})

	go func() {
		resp := httptest.NewRecorder()
		s.Handler(resp, rawlogRequest(t))
	}()
	<-entered

	closed := make(chan struct{})
	go func() { defer close(closed); _ = s.Close() }()

	select {
	case <-closed:
		t.Fatal("Close returned while a request was still processing")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-closed:
		require.True(t, finished, "in flight work completed before Close returned")
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the request finished")
	}
}

// Once closing, new requests must be rejected rather than running against
// components that are about to be torn down.
func TestRequestsRejectedWhileClosing(t *testing.T) {
	var called bool
	s := NewLogsService(LogsServiceOpts{
		HealthChecker: fakeHealthChecker{healthy: true},
		ProcessBatch: func(ctx context.Context, batch *types.LogBatch) (int64, error) {
			called = true
			types.DisposeLogBatch(batch)
			return 0, nil
		},
	})
	require.NoError(t, s.Close())

	resp := httptest.NewRecorder()
	s.Handler(resp, rawlogRequest(t))

	require.Equal(t, http.StatusServiceUnavailable, resp.Code)
	require.False(t, called, "no work is started once closing")
}

// Concurrent requests must all be awaited.
func TestCloseWaitsForAllConcurrentRequests(t *testing.T) {
	var active, peak int
	var mu sync.Mutex
	release := make(chan struct{})

	s := NewLogsService(LogsServiceOpts{
		HealthChecker: fakeHealthChecker{healthy: true},
		ProcessBatch: func(ctx context.Context, batch *types.LogBatch) (int64, error) {
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			mu.Unlock()
			<-release
			mu.Lock()
			active--
			mu.Unlock()
			types.DisposeLogBatch(batch)
			return 0, nil
		},
	})

	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Handler(httptest.NewRecorder(), rawlogRequest(t))
		}()
	}
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return active == 4 }, 2*time.Second, time.Millisecond)

	closed := make(chan struct{})
	go func() { defer close(closed); _ = s.Close() }()
	close(release)

	<-closed
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, active, "every request finished before Close returned")
	require.Equal(t, 4, peak)
}
