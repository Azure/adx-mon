package metrics

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/Azure/adx-mon/transform"
	"github.com/stretchr/testify/require"
)

// TestHandler_HandleReceive_RejectsOversizedBody demonstrates that the
// remote-write handler bounds the request body it reads. Before the fix the
// handler read the entire body into a buffer with no upper bound, so a client
// could force an arbitrarily large per-request allocation. After the fix the
// body is read through io.LimitReader with a cap, and an oversized body is
// rejected with 413 Request Entity Too Large instead of being fully read.
func TestHandler_HandleReceive_RejectsOversizedBody(t *testing.T) {
	orig := maxRemoteWriteBodyBytes
	maxRemoteWriteBodyBytes = 64 << 10 // 64 KiB
	defer func() { maxRemoteWriteBodyBytes = orig }()

	called := false
	writer := &fakeRequestWriter{
		fn: func(ctx context.Context, wr *prompb.WriteRequest) error {
			called = true
			return nil
		},
	}

	h := NewHandler(HandlerOpts{
		RequestTransformer: &transform.RequestTransformer{},
		RequestWriters:     []remote.RemoteWriteClient{writer},
		HealthChecker:      &fakeHealthChecker{healthy: true},
		Database:           "adxmetrics",
	})

	// A body larger than the cap. The content is arbitrary bytes; the point is
	// the handler's body-handling path, not a valid remote-write payload. The
	// handler snappy-decodes it, so a body that is not valid snappy would error
	// with 400 after being fully read — which is exactly the pre-fix behavior
	// we want to detect (it reads the whole body, then fails to decode).
	oversized := bytes.Repeat([]byte{0x01}, 512<<10) // 512 KiB, 8x the cap
	body := io.LimitReader(bytes.NewReader(oversized), int64(len(oversized)))

	req, err := http.NewRequest("POST", "http://localhost:8080/receive", body)
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "snappy")

	resp := httptest.NewRecorder()
	h.HandleReceive(resp, req)

	// The oversized body must be rejected with 413, not fully read and decoded.
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.Code,
		"oversized remote-write body must be rejected with 413, got %d: %s", resp.Code, resp.Body.String())
	require.False(t, called, "the request writer must not be called for an oversized body")
}
