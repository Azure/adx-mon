package collector

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/scrapetls"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/version"
	prom_model "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	caPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type MetricsClient struct {
	opts      ClientOpts
	transport *http.Transport
	client    *http.Client
	userAgent string

	profilesMu sync.Mutex
	profiles   map[string]*scrapeTLSProfileState

	closing   chan struct{}
	closeOnce sync.Once

	mu       sync.RWMutex
	token    string
	NodeName string
}

// scrapeTLSProfileState is guarded by MetricsClient.profilesMu.  A profile whose files are missing or invalid is
// unavailable: its targets fail closed and its files are rechecked at most once per refresh interval.
type scrapeTLSProfileState struct {
	profile  scrapetls.Profile
	client   *http.Client
	username string
	err      error
	checked  time.Time
}

type ClientOpts struct {
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	ScrapeTimeOut       time.Duration
	NodeName            string

	// ScrapeTLSProfiles are the named Basic auth over TLS identities that targets may select.
	ScrapeTLSProfiles map[string]scrapetls.Profile

	// ScrapeTLSRefreshInterval bounds how often the files of a profile are rechecked.  Defaults to 30s.
	ScrapeTLSRefreshInterval time.Duration
}

func (c ClientOpts) WithDefaults() ClientOpts {
	opts := ClientOpts{
		DialTimeout:         5 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ScrapeTimeOut:       10 * time.Second,

		ScrapeTLSRefreshInterval: 30 * time.Second,
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
	if c.ScrapeTLSRefreshInterval > 0 {
		opts.ScrapeTLSRefreshInterval = c.ScrapeTLSRefreshInterval
	}
	opts.NodeName = c.NodeName
	opts.ScrapeTLSProfiles = c.ScrapeTLSProfiles
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
		userAgent: fmt.Sprintf("adx-mon-collector/%s", version.String()),
		token:     token,
		closing:   make(chan struct{}),
	}

	if err := c.initProfiles(); err != nil {
		c.Close()
		return nil, err
	}

	if token != "" {
		go c.refreshToken()
	}

	return c, nil
}

func (c *MetricsClient) FetchMetrics(target string) (map[string]*prom_model.MetricFamily, error) {
	return c.FetchMetricsWithProfile(target, "")
}

// FetchMetricsWithProfile fetches target using the named scrape TLS profile.  An empty profile uses the default client.
func (c *MetricsClient) FetchMetricsWithProfile(target, profile string) (map[string]*prom_model.MetricFamily, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)

	ctx, cancel := context.WithTimeout(context.Background(), c.opts.ScrapeTimeOut)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", target, err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.do(req, target, profile)
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

func (c *MetricsClient) FetchMetricsIterator(ctx context.Context, target string) (*prompb.Iterator, error) {
	return c.FetchMetricsIteratorWithProfile(ctx, target, "")
}

// FetchMetricsIteratorWithProfile fetches target using the named scrape TLS profile.  An empty profile uses the
// default client.
func (c *MetricsClient) FetchMetricsIteratorWithProfile(ctx context.Context, target, profile string) (*prompb.Iterator, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", target, err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.do(req, target, profile)
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
			resp.Body.Close()
			return nil, fmt.Errorf("collect node metrics for %s: %w", target, err)
		}
	}
	return prompb.NewIterator(br), nil
}

// do sends req with the client selected by profile.  The service account token is only sent without a profile, and
// a profiled request never falls back to the default client.
func (c *MetricsClient) do(req *http.Request, target, profile string) (*http.Response, error) {
	client, err := c.clientForProfile(target, profile)
	if err != nil {
		return nil, err
	}

	if profile == "" {
		c.mu.RLock()
		token := c.token
		c.mu.RUnlock()

		if token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		}
	}

	return client.Do(req)
}

func (c *MetricsClient) Close() error {
	c.closeOnce.Do(func() {
		close(c.closing)
		c.client.CloseIdleConnections()

		c.profilesMu.Lock()
		defer c.profilesMu.Unlock()
		for name, state := range c.profiles {
			if state.client != nil {
				state.client.Transport.(*scrapetls.Transport).Close()
			}
			c.setProfileUsernameLocked(name, state, "")
		}
	})
	return nil
}

// initProfiles validates the structure of each profile and attempts to load it.  Structural errors are fatal; file
// errors only leave the profile unavailable until a later retry succeeds.
func (c *MetricsClient) initProfiles() error {
	c.profiles = make(map[string]*scrapeTLSProfileState, len(c.opts.ScrapeTLSProfiles))
	for name, profile := range c.opts.ScrapeTLSProfiles {
		if name == "" {
			return errors.New("scrape TLS profile name must not be empty")
		}
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("scrape TLS profile %q: %w", name, err)
		}
		c.profiles[name] = &scrapeTLSProfileState{profile: profile}
	}

	c.profilesMu.Lock()
	defer c.profilesMu.Unlock()
	now := time.Now()
	for name, state := range c.profiles {
		c.refreshProfileLocked(name, state, now)
	}
	return nil
}

// validateProfileTarget checks that target may use profile.  It does not check whether the profile is available.
func (c *MetricsClient) validateProfileTarget(target, profile string) error {
	if profile == "" {
		return nil
	}
	if _, ok := c.profiles[profile]; !ok {
		return fmt.Errorf("unknown scrape TLS profile %q", profile)
	}
	return scrapetls.ValidateTarget(target)
}

func (c *MetricsClient) clientForProfile(target, profile string) (*http.Client, error) {
	if profile == "" {
		return c.client, nil
	}
	if err := c.validateProfileTarget(target, profile); err != nil {
		return nil, err
	}

	c.profilesMu.Lock()
	defer c.profilesMu.Unlock()
	state := c.profiles[profile]
	if now := time.Now(); now.Sub(state.checked) >= c.opts.ScrapeTLSRefreshInterval {
		c.refreshProfileLocked(profile, state, now)
	}
	if state.err != nil {
		return nil, fmt.Errorf("scrape TLS profile %q unavailable: %w", profile, state.err)
	}
	return state.client, nil
}

func (c *MetricsClient) refreshProfileLocked(name string, state *scrapeTLSProfileState, now time.Time) {
	state.checked = now
	username, err := state.profile.CheckFiles()
	if err == nil && c.isClosed() {
		err = errors.New("metrics client closed")
	}
	if err == nil && state.client == nil {
		state.client, err = c.newProfileClient(state.profile)
	}
	if err != nil {
		if state.err == nil {
			logger.Warnf("Scrape TLS profile %q unavailable; its targets will not be scraped until it loads: %s", name, err)
		}
		state.err = err
		c.setProfileUsernameLocked(name, state, "")
		return
	}
	if state.err != nil {
		logger.Infof("Scrape TLS profile %q loaded", name)
	}
	state.err = nil
	c.setProfileUsernameLocked(name, state, username)
}

func (c *MetricsClient) isClosed() bool {
	select {
	case <-c.closing:
		return true
	default:
		return false
	}
}

func (c *MetricsClient) newProfileClient(profile scrapetls.Profile) (*http.Client, error) {
	client, err := scrapetls.NewClient(profile, c.opts.DialTimeout)
	if err != nil {
		return nil, err
	}
	client.Timeout = c.opts.ScrapeTimeOut
	return client, nil
}

// setProfileUsernameLocked publishes the username of an available profile.  The password is never exported.
func (c *MetricsClient) setProfileUsernameLocked(name string, state *scrapeTLSProfileState, username string) {
	if state.username != "" && state.username != username {
		metrics.CollectorScrapeTLSProfileInfo.DeleteLabelValues(name, state.username)
	}
	state.username = username
	if username != "" {
		metrics.CollectorScrapeTLSProfileInfo.WithLabelValues(name, username).Set(1)
	}
}

func (c *MetricsClient) readToken() (string, error) {
	b, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (c *MetricsClient) refreshToken() {
	t := time.NewTicker(1 * time.Minute)
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
