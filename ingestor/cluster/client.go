package cluster

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/net/http2"
)

var (
	ErrPeerOverloaded = fmt.Errorf("peer overloaded")
	ErrSegmentExists  = fmt.Errorf("segment already exists")
)

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	var kustoTransport *http.Transport
	// clone DefaultTransport, and only modify what we need.
	// we used to create new, and a missing property (NextProtos) for TLSClientConfig could block connection reuse.
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transport is not http.Transport")
	}
	kustoTransport = defaultTransport.Clone()
	kustoTransport.MaxIdleConns = 64
	kustoTransport.MaxIdleConnsPerHost = 64
	kustoTransport.MaxConnsPerHost = 64
	if kustoTransport.TLSClientConfig == nil {
		kustoTransport.TLSClientConfig = &tls.Config{}
	}
	kustoTransport.TLSClientConfig.MinVersion = tls.VersionTLS12
	kustoTransport.TLSClientConfig.Renegotiation = tls.RenegotiateNever
	kustoTransport.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify
	kustoTransport.ResponseHeaderTimeout = 55 * time.Second
	h2Transport, err := http2.ConfigureTransports(kustoTransport)
	if err != nil {
		return nil, fmt.Errorf("configure http2 transport: %w", err)
	}
	// We use the following settings to enable HTTP2 healthcheck for idle h2 connections.
	// For a broken idle TCP connection, we expect to close it after ReadIdleTimeout + PingTimeout.
	// This is helpful to avoid the HTTP client from reusing an idle broken connection.
	// Refer to the upstream issues for more details:
	// - https://github.com/golang/go/issues/59690
	// - https://github.com/Azure/azure-sdk-for-go-extensions/pull/29
	// ReadIdleTimeout is the timeout after which a health check using ping
	// frame will be carried out if no frame is received on the connection.
	// Note that a ping response will is considered a received frame, so if
	// there is no other traffic on the connection, the health check will
	// be performed every ReadIdleTimeout interval.
	// If zero, no health check is performed.
	// For safety, set it to 1 minute, matching ARM's timeout for synchronous calls.
	h2Transport.ReadIdleTimeout = time.Minute
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: h2Transport,
	}

	return &Client{
		httpClient: httpClient,
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

		return fmt.Errorf("write failed: %s", string(body))
	}
	return nil
}
