package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/adx-mon/metrics"
)

// Client is a client for alert notification services.  Notification services implement the JSON http API
// to receive alerts from the alerter service.
type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) (*Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: metrics.NewRoundTripper(t),
	}

	return &Client{
		httpClient: httpClient,
	}, nil
}

func (c *Client) Create(ctx context.Context, endpoint string, alert Alert) error {
	b, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "adx-mon-alerter")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusCreated {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read resp: %w", err)
		}
		return fmt.Errorf("write failed: %s:%s", resp.Status, string(body))
	}
	return nil
}
