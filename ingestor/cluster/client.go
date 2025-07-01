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
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/davidnarayan/go-flake"
	"github.com/klauspost/compress/gzip"
)

// PeerOverloadedError represents a peer overloaded error with optional requestId.
type PeerOverloadedError struct {
	requestId string
}

func (e *PeerOverloadedError) Error() string {
	msg := "peer overloaded"
	if e.requestId != "" {
		msg += " requestId=" + e.requestId
	}
	return msg
}
func (e *PeerOverloadedError) Is(target error) bool {
	_, ok := target.(*PeerOverloadedError)
	return ok
}
func (e *PeerOverloadedError) WithRequestId(id string) error {
	return &PeerOverloadedError{requestId: id}
}

var ErrPeerOverloaded error = &PeerOverloadedError{}

// SegmentExistsError represents a segment already exists error with optional requestId.
type SegmentExistsError struct {
	requestId string
}

func (e *SegmentExistsError) Error() string {
	msg := "segment already exists"
	if e.requestId != "" {
		msg += " requestId=" + e.requestId
	}
	return msg
}
func (e *SegmentExistsError) Is(target error) bool {
	_, ok := target.(*SegmentExistsError)
	return ok
}
func (e *SegmentExistsError) WithRequestId(id string) error {
	return &SegmentExistsError{requestId: id}
}

var ErrSegmentExists error = &SegmentExistsError{}

// SegmentLockedError represents a segment locked error with optional requestId.
type SegmentLockedError struct {
	requestId string
}

func (e *SegmentLockedError) Error() string {
	msg := "segment is locked"
	if e.requestId != "" {
		msg += " requestId=" + e.requestId
	}
	return msg
}
func (e *SegmentLockedError) Is(target error) bool {
	_, ok := target.(*SegmentLockedError)
	return ok
}
func (e *SegmentLockedError) WithRequestId(id string) error {
	return &SegmentLockedError{requestId: id}
}

var ErrSegmentLocked error = &SegmentLockedError{}

type ErrBadRequest struct {
	Msg       string
	requestId string
}

func (e *ErrBadRequest) Error() string {
	msg := fmt.Sprintf("bad request: %s", e.Msg)
	if e.requestId != "" {
		msg += " requestId=" + e.requestId
	}
	return msg
}
func (e *ErrBadRequest) Is(target error) bool {
	_, ok := target.(*ErrBadRequest)
	return ok
}
func (e *ErrBadRequest) WithRequestId(id string) error {
	return &ErrBadRequest{Msg: e.Msg, requestId: id}
}

type Client struct {
	httpClient *http.Client
	opts       ClientOpts
	idGen      *flake.Flake
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

	// DisableGzip controls whether the client disables gzip compression.
	DisableGzip bool
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

	idGen, err := flake.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create ID generator: %w", err)
	}

	return &Client{
		httpClient: httpClient,
		opts:       opts,

		idGen: idGen,
	}, nil
}

// Write writes the given paths to the given endpoint.  If multiple paths are given, they are
// merged into the first file at the destination.  This ensures we transfer the full batch
// atomimcally.
func (c *Client) Write(ctx context.Context, endpoint string, filename string, body io.Reader) error {
	rid := c.idGen.NextId()
	requestId := rid.String()

	br := bufio.NewReaderSize(body, 4*1024)
	// Send the body with gzip compression unless the client has that option disabled.
	if !c.opts.DisableGzip {
		gzipReader, gzipWriter := io.Pipe()
		go func() {
			defer gzipWriter.Close()
			gzipCompressor := gzip.NewWriter(gzipWriter)
			defer gzipCompressor.Close()
			if _, err := io.Copy(gzipCompressor, body); err != nil {
				if err := gzipWriter.CloseWithError(err); err != nil {
					logger.Errorf("failed to close gzip writer: %s requestId=%s", err, requestId)
				}
			}
		}()
		br.Reset(gzipReader)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/transfer", endpoint), br)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	params := req.URL.Query()
	params.Add("filename", filename)
	req.URL.RawQuery = params.Encode()

	if !c.opts.DisableGzip {
		req.Header.Set("Content-Encoding", "gzip")
	}

	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("User-Agent", "adx-mon")
	req.Header.Set("X-Request-ID", requestId)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w requestId=%s", err, requestId)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != 202 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read resp: %w requestId=%s", err, requestId)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if perr, ok := ErrPeerOverloaded.(*PeerOverloadedError); ok {
				return perr.WithRequestId(requestId)
			}
			return ErrPeerOverloaded
		}

		if resp.StatusCode == http.StatusConflict {
			if serr, ok := ErrSegmentExists.(*SegmentExistsError); ok {
				return serr.WithRequestId(requestId)
			}
			return ErrSegmentExists
		}

		if resp.StatusCode == http.StatusLocked {
			if lerr, ok := ErrSegmentLocked.(*SegmentLockedError); ok {
				return lerr.WithRequestId(requestId)
			}
			return ErrSegmentLocked
		}

		if resp.StatusCode == http.StatusBadRequest {
			return (&ErrBadRequest{Msg: fmt.Sprintf("write failed: %s", strings.TrimSpace(string(body)))}).WithRequestId(requestId)
		}

		return fmt.Errorf("write failed: %s requestId=%s", strings.TrimSpace(string(body)), requestId)
	}
	return nil
}
