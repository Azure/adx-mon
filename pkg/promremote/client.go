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
}

func NewClient(timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 5
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute
	t.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: t,
	}

	return &Client{
		httpClient: httpClient,
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
