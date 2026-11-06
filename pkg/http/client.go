package http

import (
	"crypto/tls"
	"net/http"
	"time"
)

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

func NewClient(opts ClientOpts) *http.Client {
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

	t.Protocols = new(http.Protocols)
	t.Protocols.SetHTTP1(true)
	if opts.DisableHTTP2 {
		t.ForceAttemptHTTP2 = false
		// The cloned DefaultTransport TLS config already advertises "h2" via ALPN.
		t.TLSClientConfig.NextProtos = []string{"http/1.1"}
	} else {
		t.Protocols.SetHTTP2(true)
		t.HTTP2 = &http.HTTP2Config{
			// if the connection has been idle for 10 seconds, send a ping frame for a health check
			SendPingTimeout: 10 * time.Second,
			// if there's no response to the ping within 2 seconds, close the connection
			PingTimeout: 2 * time.Second,
		}
	}

	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: t,
	}
}
