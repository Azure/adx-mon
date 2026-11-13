package otlp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

// A body larger than the limit must be rejected with 413 rather than read
// into an unbounded buffer sized from the client-supplied Content-Length.
// The handler's real cap is 64 MiB; this test temporarily lowers it to
// 64 KiB (restored in cleanup) and sends a 512 KiB body: 8x over the lowered
// cap, but small enough to keep the test fast.
func TestMetricsService_RejectsOversizedBody(t *testing.T) {
	orig := maxMetricsBodyBytes
	maxMetricsBodyBytes = 64 << 10 // 64 KiB
	defer func() { maxMetricsBodyBytes = orig }()

	s := NewMetricsService(metricWriterWithError(nil), "/fake/path", 0)
	resp := httptest.NewRecorder()

	body := make([]byte, 512<<10) // 512 KiB, 8x over the lowered cap
	req := httptest.NewRequest("POST", "/fake/path", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")

	s.Handler(resp, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.Code,
		"oversized body must be rejected, not fully read and unmarshalled")
	require.Equal(t, "application/x-protobuf", resp.Header().Get("Content-Type"))
	var status status.Status
	require.NoError(t, proto.Unmarshal(resp.Body.Bytes(), &status))
	require.Contains(t, status.GetMessage(), "larger than")
}
