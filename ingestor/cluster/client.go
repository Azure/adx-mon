package cluster

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Azure/adx-mon/pkg/wal/file"
)

var ErrPeerOverloaded = fmt.Errorf("peer overloaded")

type Client struct {
	httpClient      *http.Client
	storageProvider file.File
}

type ClientOptions struct {
	Timeout            time.Duration
	InsecureSkipVerify bool
	StorageProvider    file.File
}

func NewClient(opts *ClientOptions) (*Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxConnsPerHost = 100
	t.MaxIdleConnsPerHost = 100
	t.ResponseHeaderTimeout = opts.Timeout
	t.IdleConnTimeout = time.Minute
	t.TLSClientConfig.InsecureSkipVerify = opts.InsecureSkipVerify

	httpClient := &http.Client{
		Timeout:   opts.Timeout,
		Transport: t,
	}

	if opts.StorageProvider == nil {
		opts.StorageProvider = &file.Disk{}
	}

	return &Client{
		httpClient:      httpClient,
		storageProvider: opts.StorageProvider,
	}, nil
}

func (c *Client) Write(ctx context.Context, endpoint string, path string) error {
	f, err := c.storageProvider.Open(path)
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
