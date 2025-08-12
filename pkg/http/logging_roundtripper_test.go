package http

import (
	"bytes"
	"errors"
	stdhttp "net/http"
	"strings"
	"testing"

	"log/slog"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/stretchr/testify/require"
)

// fakeRT is a fake RoundTripper for testing
type fakeRT struct {
	resp   *stdhttp.Response
	err    error
	called int
}

func (f *fakeRT) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	f.called++
	return f.resp, f.err
}

// withCapturedLogs replaces the default slog logger to capture output into a buffer.
func withCapturedLogs(t *testing.T, level slog.Level) (*bytes.Buffer, func()) {
	t.Helper()

	// Ensure error-level logs are enabled (and optionally more for future use).
	prevLevel := slog.LevelInfo
	buf := &bytes.Buffer{}

	// Remember previous logger to restore later.
	prev := logger.Logger()

	// Build a text handler to make substring assertions easier.
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))

	// Set project logger level accordingly and restore everything on cleanup.
	// Note: saving prev level rigorously would require API; using INFO as sane default.
	logger.SetLevel(level)

	cleanup := func() {
		slog.SetDefault(prev)
		logger.SetLevel(prevLevel)
	}
	t.Cleanup(cleanup)
	return buf, cleanup
}

func TestLoggingRoundTripper_TransportError(t *testing.T) {
	buf, _ := withCapturedLogs(t, slog.LevelDebug)

	// Request with SAS signature to verify redaction
	req, err := stdhttp.NewRequest("GET", "https://example.com/upload?comp=block&sig=supersecret", nil)
	require.NoError(t, err)
	// Add whitelisted/sensitive headers to ensure only allowed ones appear
	req.Header.Set("x-ms-client-request-id", "crid-123")
	req.Header.Set("traceparent", "00-abc-xyz-01")
	req.Header.Set("Authorization", "Bearer SECRET")

	lrt := loggingRoundTripper{next: &fakeRT{err: errors.New("boom")}}
	resp, rerr := lrt.RoundTrip(req)

	require.Error(t, rerr)
	require.Nil(t, resp)

	out := buf.String()
	require.Contains(t, out, "HTTP transport error")
	require.Contains(t, out, "method=GET")
	require.Contains(t, out, "url=")
	require.Contains(t, out, "sig=REDACTED")
	require.NotContains(t, out, "supersecret")
	// Correlation IDs present
	require.Contains(t, out, "crid=crid-123")
	require.Contains(t, out, "tp=00-abc-xyz-01")
	// Sensitive headers must not be logged
	require.NotContains(t, out, "Authorization")
	require.NotContains(t, out, "Bearer SECRET")
}

func TestLoggingRoundTripper_Status5xx(t *testing.T) {
	buf, _ := withCapturedLogs(t, slog.LevelDebug)

	req, err := stdhttp.NewRequest("POST", "https://contoso.blob.core.windows.net/c?Sig=topsecret&x=1", strings.NewReader(""))
	require.NoError(t, err)
	// Add request headers
	req.Header.Set("x-ms-client-request-id", "crid-456")
	req.Header.Set("Authorization", "Bearer SECRET2")

	// Simulate response headers from Azure
	respHeaders := stdhttp.Header{}
	respHeaders.Set("x-ms-request-id", "rid-999")
	respHeaders.Set("x-ms-activity-id", "aid-777")
	respHeaders.Set("traceparent", "00-def-uvw-01")
	rt := &fakeRT{resp: &stdhttp.Response{StatusCode: 500, Status: "500 Internal Server Error", Request: req, Body: stdhttp.NoBody, Header: respHeaders}}
	lrt := loggingRoundTripper{next: rt}
	resp, rerr := lrt.RoundTrip(req)

	require.NoError(t, rerr)
	require.NotNil(t, resp)

	out := buf.String()
	require.Contains(t, out, "HTTP status=500")
	require.Contains(t, out, "method=POST")
	require.Contains(t, out, "sig=REDACTED")
	require.NotContains(t, out, "topsecret")
	// Correlation IDs present
	require.Contains(t, out, "crid=crid-456")
	require.Contains(t, out, "rid=rid-999")
	require.Contains(t, out, "aid=aid-777")
	require.Contains(t, out, "tp=00-def-uvw-01")
	// Sensitive headers must not be logged
	require.NotContains(t, out, "Authorization")
	require.NotContains(t, out, "Bearer SECRET2")
}

func TestLoggingRoundTripper_Status2xx_NoLog(t *testing.T) {
	buf, _ := withCapturedLogs(t, slog.LevelDebug)

	req, err := stdhttp.NewRequest("PUT", "https://example.com/api", strings.NewReader("{}"))
	require.NoError(t, err)

	rt := &fakeRT{resp: &stdhttp.Response{StatusCode: 204, Status: "204 No Content", Request: req, Body: stdhttp.NoBody}}
	lrt := loggingRoundTripper{next: rt}
	resp, rerr := lrt.RoundTrip(req)

	require.NoError(t, rerr)
	require.NotNil(t, resp)

	// Expect no error logs for 2xx statuses
	require.Equal(t, "", buf.String())
}

func TestWithLogging_WrapsDefaultWhenNil(t *testing.T) {
	c := WithLogging(nil)
	require.NotNil(t, c)
	require.NotNil(t, c.Transport)

	lrt, ok := c.Transport.(loggingRoundTripper)
	require.True(t, ok, "transport should be loggingRoundTripper")
	require.Equal(t, stdhttp.DefaultTransport, lrt.next)
}

func TestWithLogging_PreservesCustomTransport(t *testing.T) {
	base := &fakeRT{}
	c := &stdhttp.Client{Transport: base}
	got := WithLogging(c)
	require.Same(t, c, got)

	lrt, ok := got.Transport.(loggingRoundTripper)
	require.True(t, ok)
	require.Same(t, base, lrt.next)
}

func TestHasSig(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://x/y?sig=abc", true},
		{"https://x/y?Sig=abc", true},
		{"https://x/y?SIG=abc", true},
		{"https://x/y?nosig=true", false}, // does not contain standalone "sig=" param
		{"https://x/y?signature=abc", false},
		{"https://x/y", false},
	}
	for _, tc := range cases {
		got := hasSig(tc.in)
		require.Equalf(t, tc.want, got, "input: %s", tc.in)
	}
}
