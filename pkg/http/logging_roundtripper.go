package http

import (
	stdhttp "net/http"
	"regexp"
	"strings"

	"github.com/Azure/adx-mon/pkg/logger"
)

// sigRedactRe matches a SAS signature parameter value case-insensitively.
var sigRedactRe = regexp.MustCompile(`(?i)sig=[^&\s]+`)

// loggingRoundTripper wraps another RoundTripper and logs only errors.
type loggingRoundTripper struct {
	next stdhttp.RoundTripper
}

func (l loggingRoundTripper) RoundTrip(req *stdhttp.Request) (*stdhttp.Response, error) {
	resp, err := l.next.RoundTrip(req)

	if err != nil {
		url := req.URL.String()
		if hasSig(url) {
			url = sigRedactRe.ReplaceAllString(url, "sig=REDACTED")
		}
		// Capture safe correlation IDs for troubleshooting (no auth headers).
		// References for where these come from in the Kusto SDK:
		//  - x-ms-client-request-id is set by azure-kusto-go in kusto/conn.go (ClientRequestIdHeader const and getHeaders()).
		//  - Azure services commonly return x-ms-request-id (see Azure SDKs) and ADX may return x-ms-activity-id/root-activity-id.
		// Request-scope IDs
		crid := req.Header.Get("x-ms-client-request-id")
		tp := req.Header.Get("traceparent")
		// Build a compact suffix with only non-empty values
		var extra []string
		if crid != "" {
			extra = append(extra, "crid="+crid)
		}
		if tp != "" {
			extra = append(extra, "tp="+tp)
		}
		if len(extra) > 0 {
			logger.Errorf("HTTP transport error method=%s url=%s %s: %v", req.Method, url, strings.Join(extra, " "), err)
		} else {
			logger.Errorf("HTTP transport error method=%s url=%s: %v", req.Method, url, err)
		}
		return resp, err
	}

	if resp != nil && resp.StatusCode >= 400 {
		url := req.URL.String()
		if hasSig(url) {
			url = sigRedactRe.ReplaceAllString(url, "sig=REDACTED")
		}
		// Extract safe correlation IDs from response headers (Azure/Kusto conventions) and request header.
		// See notes above for sources in the Kusto SDK and Azure conventions.
		crid := req.Header.Get("x-ms-client-request-id")
		rid := pickHeader(resp.Header, "x-ms-request-id", "request-id")
		aid := pickHeader(resp.Header, "x-ms-activity-id", "x-ms-root-activity-id")
		tp := pickHeader(resp.Header, "traceparent")
		var extra []string
		if crid != "" {
			extra = append(extra, "crid="+crid)
		}
		if rid != "" {
			extra = append(extra, "rid="+rid)
		}
		if aid != "" {
			extra = append(extra, "aid="+aid)
		}
		if tp != "" {
			extra = append(extra, "tp="+tp)
		}
		if len(extra) > 0 {
			logger.Errorf("HTTP status=%d method=%s url=%s %s", resp.StatusCode, req.Method, url, strings.Join(extra, " "))
		} else {
			logger.Errorf("HTTP status=%d method=%s url=%s", resp.StatusCode, req.Method, url)
		}
	}
	return resp, nil
}

// WithLogging wraps the client's Transport to log only errors (transport failures and HTTP >= 400).
func WithLogging(c *stdhttp.Client) *stdhttp.Client {
	if c == nil {
		c = &stdhttp.Client{}
	}
	next := c.Transport
	if next == nil {
		next = stdhttp.DefaultTransport
	}
	c.Transport = loggingRoundTripper{next: next}
	return c
}

// hasSig is a cheap pre-check to avoid running regex on the hot path when not needed.
func hasSig(s string) bool {
	// Case-insensitive contains check for "sig=" without allocations.
	// Fast path for common lowercase.
	if strings.Contains(s, "sig=") {
		return true
	}
	// Coarse fallback to avoid bringing unicode/lowercasing: check a few common variants.
	return strings.Contains(s, "Sig=") || strings.Contains(s, "SIG=")
}

// pickHeader returns the first non-empty value among the provided header keys.
// This is purposely restricted to explicit, non-sensitive headers.
func pickHeader(h stdhttp.Header, keys ...string) string {
	for _, k := range keys {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}
