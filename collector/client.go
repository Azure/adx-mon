package collector

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	prom_model "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"
)

const (
	caPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type MetricsClient struct {
	opts      ClientOpts
	transport *http.Transport
	client    *http.Client

	closing chan struct{}

	mu       sync.RWMutex
	token    string
	NodeName string
}

type ClientOpts struct {
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	ScrapeTimeOut       time.Duration
	NodeName            string
}

func (c ClientOpts) WithDefaults() ClientOpts {
	opts := ClientOpts{
		DialTimeout:         5 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ScrapeTimeOut:       10 * time.Second,
	}
	if c.ScrapeTimeOut.Seconds() > 0 {
		opts.ScrapeTimeOut = c.ScrapeTimeOut
	}

	if c.DialTimeout.Seconds() > 0 {
		opts.DialTimeout = c.DialTimeout
	}

	if c.TLSHandshakeTimeout.Seconds() > 0 {
		opts.TLSHandshakeTimeout = c.TLSHandshakeTimeout
	}
	opts.NodeName = c.NodeName
	return opts
}

func NewMetricsClient(opts ClientOpts) (*MetricsClient, error) {
	opts = opts.WithDefaults()
	dialer := &net.Dialer{
		Timeout: opts.DialTimeout,
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: opts.TLSHandshakeTimeout,
	}

	if _, err := os.Stat(caPath); err == nil {
		// Load CA cert
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}
		caCertPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		caCertPool.AppendCertsFromPEM(caCert)

		// Setup HTTPS client
		tlsConfig := &tls.Config{
			RootCAs: caCertPool,

			// Match how K8s handles probing with self-signed certs
			// https://github.com/kubernetes/kubernetes/blob/master/pkg/probe/http/http.go#L41
			InsecureSkipVerify: true,
		}
		transport.TLSClientConfig = tlsConfig
	}

	var token string
	if _, err := os.Stat(tokenPath); err == nil {
		b, err := os.ReadFile(tokenPath)
		if err != nil {
			return nil, err
		}
		token = string(b)
	}

	httpClient := &http.Client{
		Timeout:   opts.ScrapeTimeOut,
		Transport: transport,
	}

	c := &MetricsClient{
		opts:      opts,
		NodeName:  opts.NodeName,
		transport: transport,
		client:    httpClient,
		token:     token,
		closing:   make(chan struct{}),
	}

	if token != "" {
		go c.refreshToken()
	}

	return c, nil
}

func (c *MetricsClient) FetchMetrics(target string) (map[string]*prom_model.MetricFamily, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)

	ctx, cancel := context.WithTimeout(context.Background(), c.opts.ScrapeTimeOut)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", target, err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("collect node metrics for %s: %s", target, resp.Status)
	}

	br := resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		br, err = gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
		}
		defer br.Close()
	}

	fams, err := parser.TextToMetricFamilies(br)
	if err != nil {
		return nil, fmt.Errorf("decode metrics for %s: %w", target, err)
	}
	return fams, err
}

func (c *MetricsClient) FetchMetricsIterator(target string) (*prompb.Iterator, error) {
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", target, err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("collect node metrics for %s: %s", target, resp.Status)
	}

	br := resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		br, err = NewGzipReaderWithClose(resp.Body)
		if err != nil {
			defer br.Close()
			return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
		}
	}
	return prompb.NewIterator(br), nil
}

// Pods returns a list of pods running on the node. This uses the kubelet API instead of the kubernetes API server.
func (c *MetricsClient) Pods() (corev1.PodList, error) {
	url := fmt.Sprintf("https://%s:10250/pods", c.NodeName)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return corev1.PodList{}, fmt.Errorf("failed to create request for %s: %w", url, err)
	}
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	resp, err := c.client.Do(req)
	if err != nil || resp == nil {
		return corev1.PodList{}, fmt.Errorf("failed to request %s: %w", url, err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	_, err = buf.ReadFrom(resp.Body)
	if err != nil {
		return corev1.PodList{}, fmt.Errorf("failed to read response body from %s (status: %s): %w", url, resp.Status, err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyText := buf.String()
		if len(bodyText) > 500 {
			bodyText = bodyText[:500] + "..."
		}
		return corev1.PodList{}, fmt.Errorf("kubelet API request to %s failed with status %s: %s", url, resp.Status, bodyText)
	}

	var pl corev1.PodList
	err = json.Unmarshal(buf.Bytes(), &pl)
	if err != nil {
		bodyText := buf.String()
		if len(bodyText) > 200 {
			bodyText = bodyText[:200] + "..."
		}
		return corev1.PodList{}, fmt.Errorf("failed to parse JSON response from %s (status: %s, body: %s): %w", url, resp.Status, bodyText, err)
	}
	return pl, nil
}

func (c *MetricsClient) Close() error {
	close(c.closing)
	c.client.CloseIdleConnections()
	return nil
}

func (c *MetricsClient) readToken() (string, error) {
	b, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *MetricsClient) refreshToken() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()

	for {
		select {
		case <-c.closing:
			return
		case <-t.C:
			token, err := c.readToken()
			if err != nil {
				logger.Errorf("Failed to read token: %s", err)
				continue
			}

			c.mu.Lock()
			c.token = token
			c.mu.Unlock()
		}
	}
}

// gzipReaderWithClose wraps gzip.Reader and the underlying reader
type gzipReaderWithClose struct {
	*gzip.Reader
	underlying io.ReadCloser
}

// Close closes both the gzip.Reader and the underlying reader
func (gr *gzipReaderWithClose) Close() error {
	err := gr.Reader.Close()
	if err != nil {
		return err
	}
	return gr.underlying.Close()
}

// NewGzipReaderWithClose creates a new gzipReaderWithClose
func NewGzipReaderWithClose(r io.ReadCloser) (*gzipReaderWithClose, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &gzipReaderWithClose{
		Reader:     gr,
		underlying: r,
	}, nil
}
