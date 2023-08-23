package promremote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/golang/snappy"
)

// Client is a client for the prometheus remote write API.  It is safe to be shared between goroutines.
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
	return c
}

func NewClient(opts ClientOpts) (*Client, error) {
	opts = opts.WithDefaults()
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = opts.MaxIdleConns
	t.MaxConnsPerHost = opts.MaxConnsPerHost
	t.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	t.ResponseHeaderTimeout = opts.ResponseHeaderTimeout
	t.IdleConnTimeout = opts.IdleConnTimeout
	t.TLSClientConfig.InsecureSkipVerify = opts.InsecureSkipVerify

	httpClient := &http.Client{
		Timeout:   opts.Timeout,
		Transport: t,
	}

	return &Client{
		httpClient: httpClient,
		opts:       opts,
	}, nil
}

func (c *Client) Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error {
	b, err := wr.Marshal()
	if err != nil {
		return fmt.Errorf("marshal proto: %w", err)
	}

	encoded := snappy.Encode(nil, b)
	body := bytes.NewReader(encoded)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	req.Header.Set("User-Agent", "adx-mon")
	req.Close = c.opts.Close

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode/100 != 2 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read resp: %w", err)
		}
		return fmt.Errorf("write failed: %s", string(body))
	}
	return nil
}

func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}
