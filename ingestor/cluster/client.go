package cluster

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Azure/adx-mon/metrics"
)

var ErrPeerOverloaded = fmt.Errorf("peer overloaded")

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration, insecureSkipVerify bool) (*Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute
	t.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: metrics.NewRoundTripper(metrics.IngestorSubsystem, t),
	}

	return &Client{
		httpClient: httpClient,
	}, nil
}

func (c *Client) Write(ctx context.Context, endpoint string, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	filename := filepath.Base(path)

	br := bufio.NewReaderSize(f, 1024*1024)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, br)
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

		return fmt.Errorf("write failed: %s", string(body))
	}
	return nil
}
