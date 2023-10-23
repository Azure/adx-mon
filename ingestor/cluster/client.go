package cluster

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Azure/adx-mon/pkg/wal/file"
)

var ErrPeerOverloaded = fmt.Errorf("peer overloaded")

type Client struct {
	httpClient      *http.Client
	storageProvider file.Provider
}

func NewClient(timeout time.Duration, insecureSkipVerify bool, storageProvider file.Provider) (*Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	t.ResponseHeaderTimeout = timeout
	t.IdleConnTimeout = time.Minute
	t.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: t,
	}

	return &Client{
		httpClient:      httpClient,
		storageProvider: storageProvider,
	}, nil
}

// Write writes the given paths to the given endpoint.  If multiple paths are given, they are
// merged into the first file at the destination.  This ensures we transfer the full batch
// atomimcally.
func (c *Client) Write(ctx context.Context, endpoint string, filename string, body io.Reader) error {

	br := bufio.NewReaderSize(body, 4*1024)

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
