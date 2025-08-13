package http

import (
	stdhttp "net/http"
	neturl "net/url"
	"path"
	"regexp"
	"strings"

	"github.com/Azure/adx-mon/pkg/logger"
)

// sigRedactRe matches only the "sig=" value portion (case-insensitive).
// We rely on hasSig() to ensure we only redact standalone parameters.
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
		// Try to capture the uploaded blob/file name (last URL path segment).
		// Context: When the azure-kusto-go queued ingestion path uploads to Azure Blob Storage,
		// it uses azblob.Client with container and blobName arguments. The azblob SDK encodes
		// these as URL path segments: "/<container>/<blobName>". See vendor:
		//   - github.com/Azure/azure-kusto-go/kusto/ingest/internal/queued/queued.go
		//     (Reader/localToBlob -> uploadStream/uploadBlob with container, blobName)
		//   - fullUrl() in the same file sets parseURL.ContainerName/BlobName and returns the URL.
		// Therefore, for Blob Storage requests the last path segment identifies the blob.
		// For non-blob endpoints (e.g., ADX mgmt: /v1/rest/mgmt), this will simply be the last
		// segment like "mgmt" and not a blob name.
		if base := path.Base(req.URL.Path); base != "." && base != "/" && base != "" {
			if dec, derr := neturl.PathUnescape(base); derr == nil {
				extra = append(extra, "blob="+dec)
			} else {
				extra = append(extra, "blob="+base)
			}
		}
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
		// Try to capture the uploaded blob/file name (last URL path segment).
		// See detailed note above regarding azblob path encoding and non-blob endpoints.
		if base := path.Base(req.URL.Path); base != "." && base != "/" && base != "" {
			if dec, derr := neturl.PathUnescape(base); derr == nil {
				extra = append(extra, "blob="+dec)
			} else {
				extra = append(extra, "blob="+base)
			}
		}
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
	// Only detect standalone query parameter keys: "?sig=" or "&sig=" (case-insensitive).
	// Avoid allocations by checking common case variants explicitly.
	return strings.Contains(s, "?sig=") || strings.Contains(s, "&sig=") ||
		strings.Contains(s, "?Sig=") || strings.Contains(s, "&Sig=") ||
		strings.Contains(s, "?SIG=") || strings.Contains(s, "&SIG=")
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
