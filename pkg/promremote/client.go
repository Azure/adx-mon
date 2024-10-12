package promremote

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/golang/snappy"
)

var (
	bytesPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 32*1024)
		},
	}
)

type RemoteWriteClient interface {
	Write(ctx context.Context, endpoint string, wr *prompb.WriteRequest) error
	CloseIdleConnections()
}

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
	opts = opts.WithDefaults()
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = opts.MaxIdleConns
	t.MaxConnsPerHost = opts.MaxConnsPerHost
	t.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	t.ResponseHeaderTimeout = opts.ResponseHeaderTimeout
	t.IdleConnTimeout = opts.IdleConnTimeout
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	}
	t.TLSClientConfig.InsecureSkipVerify = opts.InsecureSkipVerify
	t.TLSHandshakeTimeout = opts.TLSHandshakeTimeout
	t.DisableKeepAlives = opts.DisableKeepAlives

	if opts.DisableHTTP2 {
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}
		t.TLSClientConfig.NextProtos = []string{"http/1.1"}
	}

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
	b := bytesPool.Get().([]byte)
	defer bytesPool.Put(b)

	b, err := wr.MarshalTo(b[:0])
	if err != nil {
		return fmt.Errorf("marshal proto: %w", err)
	}

	b1 := bytesPool.Get().([]byte)
	defer bytesPool.Put(b1)

	encoded := snappy.Encode(b1[:0], b)
	body := bytes.NewReader(encoded)

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/receive", endpoint), body)
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
