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
		logger.Errorf("HTTP transport error method=%s url=%s: %v", req.Method, url, err)
		return resp, err
	}

	if resp != nil && resp.StatusCode >= 400 {
		url := req.URL.String()
		if hasSig(url) {
			url = sigRedactRe.ReplaceAllString(url, "sig=REDACTED")
		}
		logger.Errorf("HTTP status=%d method=%s url=%s", resp.StatusCode, req.Method, url)
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
