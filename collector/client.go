package collector

import (
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	prom_model "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

const (
	caPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type MetricsClient struct {
	transport *http.Transport
	client    *http.Client

	closing chan struct{}

	mu    sync.RWMutex
	token string
}

type ClientOpts struct {
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	ScrapeTimeOut       time.Duration
}

func (c ClientOpts) WithDefaults() ClientOpts {
	opts := ClientOpts{
		DialTimeout:         5 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ScrapeTimeOut:       10 * time.Second,
	}
	opts.ScrapeTimeOut = c.ScrapeTimeOut
	opts.DialTimeout = c.DialTimeout
	opts.TLSHandshakeTimeout = c.TLSHandshakeTimeout
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
	parser := &expfmt.TextParser{}

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
