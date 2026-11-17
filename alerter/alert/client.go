package alert

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// maxErrorBodyBytes caps the diagnostic body length retained on a non-201 StatusError.
	maxErrorBodyBytes = 4 * 1024

	// DefaultMaxDiscoveryBodyBytes caps the response body read from GET <base>/destinations (64 KiB).
	DefaultMaxDiscoveryBodyBytes = 64 * 1024

	// maxSecretLookaheadBytes bounds the extra lookahead read past maxErrorBodyBytes so
	// credentials spanning the 4 KiB boundary are matched and redacted before truncation.
	maxSecretLookaheadBytes = 16 * 1024

	// minStandaloneSecretLen is the minimum length for redacting individual query/path values
	// outside their structured URL/query context, avoiding short flag values such as "1" or "e".
	minStandaloneSecretLen = 6
)

var (
	// ErrTooManyRequests is returned when a given alert is sent too many times.
	ErrTooManyRequests = errors.New("too many requests")
)

// StatusError represents a non-201, non-429 HTTP response from a notification endpoint.
type StatusError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("write failed: %s:%s", e.Status, e.Body)
}

// Retryable reports whether this HTTP status represents a transient server error (5xx).
func (e *StatusError) Retryable() bool {
	return e.StatusCode >= http.StatusInternalServerError && e.StatusCode <= 599
}

type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string { return e.err.Error() }
func (e *nonRetryableError) Unwrap() error { return e.err }
func (e *nonRetryableError) Retryable() bool {
	return false
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// Client is a client for alert notification services. Notification services implement the JSON http API
// to receive alerts from the alerter service.
type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) (*Client, error) {
	return NewClientWithConcurrency(timeout, 100)
}

// NewClientWithConcurrency creates a notification HTTP client with its own isolated *http.Transport
// sized to at least max(100, concurrency) connections per host.
func NewClientWithConcurrency(timeout time.Duration, concurrency int) (*Client, error) {
	poolSize := max(100, concurrency)
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = poolSize
	t.MaxConnsPerHost = poolSize
	t.MaxIdleConnsPerHost = poolSize
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute

	httpClient := &http.Client{
		Transport: t,
		Timeout:   timeout,
	}

	return &Client{
		httpClient: httpClient,
	}, nil
}

// CloseIdleConnections closes any idle keep-alive connections held by this client's transport.
func (c *Client) CloseIdleConnections() {
	if c != nil && c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}

func (c *Client) Create(ctx context.Context, endpoint string, alert Alert) error {
	b, err := json.Marshal(alert)
	if err != nil {
		return &nonRetryableError{err: fmt.Errorf("marshal alert: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if err != nil {
		return &nonRetryableError{err: fmt.Errorf("new request: %w", sanitizeHTTPError(err, endpoint))}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "adx-mon-alerter")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", sanitizeHTTPError(err, endpoint))
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusTooManyRequests {
			return ErrTooManyRequests
		}

		// Read past the 4 KiB boundary by up to maxSecretLen, sanitize in a single pass,
		// and truncate afterwards so secrets straddling byte 4096 never leave a partial prefix.
		replacer, maxSecretLen := buildURLReplacer(endpoint)
		lookahead := min(max(256, maxSecretLen), maxSecretLookaheadBytes)
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxErrorBodyBytes+lookahead)))
		sanitizedBody := replacer.Replace(string(rawBody))
		if len(sanitizedBody) > maxErrorBodyBytes {
			sanitizedBody = sanitizedBody[:maxErrorBodyBytes]
		}
		return &StatusError{
			StatusCode: resp.StatusCode,
			Status:     localHTTPStatusLine(resp.StatusCode),
			Body:       sanitizedBody,
		}
	}
	return nil
}

func localHTTPStatusLine(code int) string {
	if text := http.StatusText(code); text != "" {
		return fmt.Sprintf("%d %s", code, text)
	}
	return strconv.Itoa(code)
}

func sanitizeHTTPError(err error, rawEndpoint string) error {
	if err == nil {
		return nil
	}
	var already *redactedError
	if errors.As(err, &already) {
		return err
	}

	redactedID := RedactURL(rawEndpoint)
	var ue *url.Error
	if errors.As(err, &ue) {
		inner := ue.Err
		replacer, _ := buildURLReplacer(rawEndpoint, ue.URL)
		innerMsg := replacer.Replace(inner.Error())
		return &redactedError{
			msg: fmt.Sprintf("%s %s: %s", ue.Op, redactedID, innerMsg),
			err: inner,
		}
	}

	replacer, _ := buildURLReplacer(rawEndpoint)
	msg := err.Error()
	sanitized := replacer.Replace(msg)
	if sanitized == msg {
		return err
	}
	return &redactedError{
		msg: sanitized,
		err: err,
	}
}

func sanitizeRawURL(text string, rawURLs ...string) string {
	if text == "" || len(rawURLs) == 0 {
		return text
	}
	replacer, _ := buildURLReplacer(rawURLs...)
	return replacer.Replace(text)
}

// SanitizeText replaces any raw webhook URLs, credentials, paths, or query tokens derived from rawURLs
// with redacted endpoint identifiers or "<redacted>" placeholders.
func SanitizeText(text string, rawURLs ...string) string {
	return sanitizeRawURL(text, rawURLs...)
}

type replaceCandidate struct {
	old      string
	new      string
	priority int // 0 = identity-protected token, 1 = redaction rule
}

// buildURLReplacer builds a single-pass strings.Replacer that never re-scans emitted
// "<redacted>" tokens or "<scheme>://<host>[:<port>]/#<8-hex>" endpoint identifiers.
func buildURLReplacer(rawURLs ...string) (*strings.Replacer, int) {
	seen := make(map[string]replaceCandidate, len(rawURLs)*8+4)
	add := func(old, repl string, priority int) {
		if old == "" {
			return
		}
		if existing, exists := seen[old]; exists {
			if priority < existing.priority {
				seen[old] = replaceCandidate{old: old, new: repl, priority: priority}
			}
			return
		}
		seen[old] = replaceCandidate{old: old, new: repl, priority: priority}
	}

	// Identity-protect redaction markers so repeated passes are strictly idempotent.
	add("<redacted>", "<redacted>", 0)
	add("/<redacted>", "/<redacted>", 0)

	var maxSecretLen int
	recordSecret := func(old, repl string) {
		if old == "" {
			return
		}
		if len(old) > maxSecretLen {
			maxSecretLen = len(old)
		}
		add(old, repl, 1)
	}

	for _, raw := range rawURLs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		id := RedactURL(trimmed)
		if strings.Contains(id, "/#") {
			// Identity-protect already-formatted endpoint IDs so neither host prefixes nor hex chars are altered.
			add(id, id, 0)
		}

		recordSecret(trimmed, id)
		if raw != trimmed {
			recordSecret(raw, id)
		}

		u, err := url.Parse(trimmed)
		if err != nil {
			continue
		}
		if redactedStd := u.Redacted(); redactedStd != "" {
			recordSecret(redactedStd, id)
		}

		hostLower := strings.ToLower(u.Hostname())
		isHost := func(val string) bool {
			low := strings.ToLower(val)
			return low == hostLower || low == strings.ToLower(u.Host)
		}

		if u.User != nil {
			if ui := u.User.String(); ui != "" {
				recordSecret(ui, "<redacted>")
			}
			if pw, ok := u.User.Password(); ok && len(pw) >= minStandaloneSecretLen && !isHost(pw) {
				recordSecret(pw, "<redacted>")
			}
			if un := u.User.Username(); len(un) >= minStandaloneSecretLen && !isHost(un) {
				recordSecret(un, "<redacted>")
			}
		}

		if reqURI := u.RequestURI(); reqURI != "" && reqURI != "/" && reqURI != "/alerts" && reqURI != "/destinations" {
			recordSecret(reqURI, "/<redacted>")
		}
		if escPath := u.EscapedPath(); escPath != "" && escPath != "/" && escPath != "/alerts" && escPath != "/destinations" {
			recordSecret(escPath, "/<redacted>")
		}
		if u.Path != "" && u.Path != "/" && u.Path != "/alerts" && u.Path != "/destinations" {
			recordSecret(u.Path, "/<redacted>")
		}
		if u.RawQuery != "" {
			recordSecret(u.RawQuery, "<redacted>")
		}

		for _, pair := range strings.Split(u.RawQuery, "&") {
			if _, val, ok := strings.Cut(pair, "="); ok && val != "" {
				if len(val) >= minStandaloneSecretLen && !isHost(val) {
					recordSecret(val, "<redacted>")
				}
				if decoded, decErr := url.QueryUnescape(val); decErr == nil && len(decoded) >= minStandaloneSecretLen && !isHost(decoded) {
					recordSecret(decoded, "<redacted>")
				}
			}
		}
		for _, seg := range strings.Split(u.Path, "/") {
			if seg != "" && seg != "alerts" && seg != "destinations" && len(seg) >= minStandaloneSecretLen && !isHost(seg) {
				recordSecret(seg, "<redacted>")
			}
		}
	}

	candidates := make([]replaceCandidate, 0, len(seen))
	for _, c := range seen {
		candidates = append(candidates, c)
	}
	slices.SortFunc(candidates, func(a, b replaceCandidate) int {
		if d := cmp.Compare(len(b.old), len(a.old)); d != 0 {
			return d
		}
		if d := cmp.Compare(a.priority, b.priority); d != 0 {
			return d
		}
		return cmp.Compare(a.old, b.old)
	})

	oldnew := make([]string, 0, len(candidates)*2)
	for _, c := range candidates {
		oldnew = append(oldnew, c.old, c.new)
	}
	return strings.NewReplacer(oldnew...), maxSecretLen
}
