package cluster

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	adxhttp "github.com/Azure/adx-mon/pkg/http"
)

var (
	ErrPeerOverloaded = fmt.Errorf("peer overloaded")
	ErrSegmentExists  = fmt.Errorf("segment already exists")
	ErrSegmentLocked  = fmt.Errorf("segment is locked")
)

type ErrBadRequest struct {
	Msg string
}

func (e ErrBadRequest) Error() string {
	return fmt.Sprintf("bad request: %s", e.Msg)
}
func (e ErrBadRequest) Is(target error) bool {
	return target == ErrBadRequest{}
}

type Client struct {
	httpClient *http.Client
	opts       ClientOpts
}

type ClientOpts struct {
	// Close controls whether the client closes the connection after each request.
	Close bool

	// Timeout is the timeout for the http client and the http request.
	Timeout time.Duration

	// InsecureSkipVerify controls whether the client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// IdleConnTimeout is the maximum amount of time an idle (keep-alive) connection
	// will remain idle before closing itself.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the amount of time to wait for a server's response headers
	// after fully writing the request (including its body, if any).
	ResponseHeaderTimeout time.Duration

	// MaxIdleConns controls the maximum number of idle (keep-alive) connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost, if non-zero, controls the maximum idle (keep-alive) per host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost, if non-zero, controls the maximum connections per host.
	MaxConnsPerHost int

	// TLSHandshakeTimeout specifies the maximum amount of time to
	// wait for a TLS handshake. Zero means no timeout.
	TLSHandshakeTimeout time.Duration

	// DisableHTTP2 controls whether the client disables HTTP/2 support.
	DisableHTTP2 bool

	// DisableKeepAlives controls whether the client disables HTTP keep-alives.
	DisableKeepAlives bool
}

func (c ClientOpts) WithDefaults() ClientOpts {
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = 1 * time.Minute
	}
	if c.ResponseHeaderTimeout == 0 {
		c.ResponseHeaderTimeout = 10 * time.Second
	}

	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 100
	}

	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = 5
	}

	if c.MaxConnsPerHost == 0 {
		c.MaxConnsPerHost = 5
	}

	if c.TLSHandshakeTimeout == 0 {
		c.TLSHandshakeTimeout = 10 * time.Second
	}

	return c
}

func NewClient(opts ClientOpts) (*Client, error) {
	httpClient := adxhttp.NewClient(
		adxhttp.ClientOpts{
			Timeout:               opts.Timeout,
			IdleConnTimeout:       opts.IdleConnTimeout,
			ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
			MaxIdleConns:          opts.MaxIdleConns,
			MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
			MaxConnsPerHost:       opts.MaxConnsPerHost,
			TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
			InsecureSkipVerify:    opts.InsecureSkipVerify,
			DisableHTTP2:          opts.DisableHTTP2,
			Close:                 opts.Close,
			DisableKeepAlives:     opts.DisableKeepAlives,
		})

	return &Client{
		httpClient: httpClient,
		opts:       opts,
	}, nil
}

// Write writes the given paths to the given endpoint.  If multiple paths are given, they are
// merged into the first file at the destination.  This ensures we transfer the full batch
// atomimcally.
func (c *Client) Write(ctx context.Context, endpoint string, filename string, body io.Reader) error {

	br := bufio.NewReaderSize(body, 4*1024)

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/transfer", endpoint), br)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	params := req.URL.Query()
	params.Add("filename", filename)
	req.URL.RawQuery = params.Encode()

	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("User-Agent", "adx-mon")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != 202 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read resp: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			return ErrPeerOverloaded
		}

		if resp.StatusCode == http.StatusConflict {
			return ErrSegmentExists
		}

		if resp.StatusCode == http.StatusLocked {
			return ErrSegmentLocked
		}

		if resp.StatusCode == http.StatusBadRequest {
			return &ErrBadRequest{Msg: fmt.Sprintf("write failed: %s", strings.TrimSpace(string(body)))}
		}

		return fmt.Errorf("write failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}
