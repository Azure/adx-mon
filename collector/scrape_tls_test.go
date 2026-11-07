package collector

import (
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/scrapetls"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/remote"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scrapeTLSFixture(t *testing.T) (scrapetls.Profile, tls.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "exporter.test"},
		DNSNames: []string{"exporter.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	p := scrapetls.Profile{
		CAFile:       filepath.Join(dir, "ca.pem"),
		UsernameFile: filepath.Join(dir, "username"),
		PasswordFile: filepath.Join(dir, "password"),
		ServerName:   "exporter.test",
	}
	require.NoError(t, os.WriteFile(p.CAFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(p.UsernameFile, []byte("synthetic-user"), 0600))
	require.NoError(t, os.WriteFile(p.PasswordFile, []byte("synthetic-password"), 0600))
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return p, pair
}

func startScrapeTLSServer(t *testing.T, pair tls.Certificate, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// useTrustedDefaultClient replaces the default client with one that trusts srv, so tests can prove a profiled
// request never falls back to it.
func useTrustedDefaultClient(c *MetricsClient, srv *httptest.Server, serverName, token string) {
	c.client = srv.Client()
	c.client.Transport.(*http.Transport).TLSClientConfig.ServerName = serverName
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

func TestScrapeTLSMetricsFetchMethods(t *testing.T) {
	profile, pair := scrapeTLSFixture(t)
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprint("gzip=", compressed), func(t *testing.T) {
			srv := startScrapeTLSServer(t, pair, func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				if !ok || username != "synthetic-user" || password != "synthetic-password" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if len(r.TLS.PeerCertificates) != 0 || r.Header.Get("Proxy-Authorization") != "" ||
					r.Header.Get("Accept-Encoding") != "gzip" || !strings.Contains(r.Header.Get("User-Agent"), "adx-mon-collector/") {
					t.Errorf("unexpected request headers: %v", r.Header)
				}
				if r.URL.Path == "/bad-gzip" {
					w.Header().Set("Content-Encoding", "gzip")
					fmt.Fprint(w, "invalid gzip")
					return
				}
				var out io.Writer = w
				if compressed {
					w.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(w)
					defer gz.Close()
					out = gz
				}
				fmt.Fprint(out, "# TYPE test_metric gauge\ntest_metric{source=\"synthetic\"} 42\n")
			})
			c, err := NewMetricsClient(ClientOpts{ScrapeTLSProfiles: map[string]scrapetls.Profile{"exporter": profile}})
			require.NoError(t, err)
			defer c.Close()
			ctx := context.Background()

			// Trusted TLS without Basic auth must fail through both APIs.
			useTrustedDefaultClient(c, srv, profile.ServerName, "")
			_, err = c.FetchMetrics(srv.URL)
			require.ErrorContains(t, err, "401")
			_, err = c.FetchMetricsIterator(ctx, srv.URL)
			require.ErrorContains(t, err, "401")

			// The service account token is not sent with a profile.
			useTrustedDefaultClient(c, srv, profile.ServerName, "synthetic-service-account-token")
			fams, err := c.FetchMetricsWithProfile(srv.URL, "exporter")
			require.NoError(t, err)
			require.Equal(t, float64(42), fams["test_metric"].Metric[0].Gauge.GetValue())
			iter, err := c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "exporter")
			require.NoError(t, err)
			require.True(t, iter.Next())
			require.False(t, iter.Next())
			require.NoError(t, iter.Err())
			require.NoError(t, iter.Close())

			_, err = c.FetchMetricsWithProfile(srv.URL+"/bad-gzip", "exporter")
			require.Error(t, err)
			_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL+"/bad-gzip", "exporter")
			require.Error(t, err)
			_, err = c.FetchMetricsWithProfile(srv.URL, "unknown")
			require.ErrorContains(t, err, "unknown scrape TLS profile")
			_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "unknown")
			require.ErrorContains(t, err, "unknown scrape TLS profile")

			// Credentials are re-read on every request.
			for _, path := range []string{profile.UsernameFile, profile.PasswordFile} {
				for _, value := range []string{"wrong", "", " \n"} {
					require.NoError(t, os.WriteFile(path, []byte(value), 0600))
					_, err = c.FetchMetricsWithProfile(srv.URL, "exporter")
					require.ErrorContains(t, err, "401")
					_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "exporter")
					require.ErrorContains(t, err, "401")
				}
				require.NoError(t, os.Remove(path))
				_, err = c.FetchMetricsWithProfile(srv.URL, "exporter")
				require.Error(t, err)
				_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "exporter")
				require.Error(t, err)
				require.NoError(t, os.Mkdir(path, 0700))
				_, err = c.FetchMetricsWithProfile(srv.URL, "exporter")
				require.Error(t, err)
				_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "exporter")
				require.Error(t, err)
				require.NoError(t, os.Remove(path))
				value := "synthetic-user"
				if path == profile.PasswordFile {
					value = "synthetic-password"
				}
				require.NoError(t, os.WriteFile(path, []byte(value), 0600))
				_, err = c.FetchMetricsWithProfile(srv.URL, "exporter")
				require.NoError(t, err)
			}
		})
	}
}

func TestScrapeTLSRejectsHTTPAndKeepsDefaultBehavior(t *testing.T) {
	profile, _ := scrapeTLSFixture(t)
	c, err := NewMetricsClient(ClientOpts{ScrapeTLSProfiles: map[string]scrapetls.Profile{"exporter": profile}})
	require.NoError(t, err)
	defer c.Close()
	c.mu.Lock()
	c.token = "synthetic-service-account-token"
	c.mu.Unlock()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") == "" {
			t.Error("default bearer token behavior changed")
		}
		fmt.Fprint(w, "test_metric 1\n")
	}))
	defer srv.Close()
	ctx := context.Background()

	_, err = c.FetchMetricsWithProfile(srv.URL, "exporter")
	require.ErrorContains(t, err, "HTTPS")
	_, err = c.FetchMetricsIteratorWithProfile(ctx, srv.URL, "exporter")
	require.ErrorContains(t, err, "HTTPS")
	require.Zero(t, calls.Load())

	fams, err := c.FetchMetrics(srv.URL)
	require.NoError(t, err)
	require.Contains(t, fams, "test_metric")
	iter, err := c.FetchMetricsIterator(ctx, srv.URL)
	require.NoError(t, err)
	for iter.Next() {
	}
	require.NoError(t, iter.Err())
	require.NoError(t, iter.Close())
	require.Equal(t, int64(2), calls.Load())
	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
}

func TestScrapeTLSRedirectsAndIsolation(t *testing.T) {
	p1, pair1 := scrapeTLSFixture(t)
	p2, pair2 := scrapeTLSFixture(t)
	var reached atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer destination.Close()
	handler := func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "synthetic-user" || p != "synthetic-password" {
			t.Error("missing Basic auth")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
			return
		}
		fmt.Fprint(w, "test_metric 1\n")
	}
	s1, s2 := startScrapeTLSServer(t, pair1, handler), startScrapeTLSServer(t, pair2, handler)
	c, err := NewMetricsClient(ClientOpts{ScrapeTLSProfiles: map[string]scrapetls.Profile{"one": p1, "two": p2}})
	require.NoError(t, err)
	defer c.Close()
	c.mu.Lock()
	c.token = "synthetic-service-account-token"
	c.mu.Unlock()
	for _, fetch := range []func(string, string) error{
		func(target, profile string) error { _, err := c.FetchMetricsWithProfile(target, profile); return err },
		func(target, profile string) error {
			iter, err := c.FetchMetricsIteratorWithProfile(context.Background(), target, profile)
			if err != nil {
				return err
			}
			for iter.Next() {
			}
			err = iter.Err()
			iter.Close()
			return err
		},
	} {
		require.NoError(t, fetch(s1.URL, "one"))
		require.NoError(t, fetch(s2.URL, "two"))
		require.Error(t, fetch(s1.URL, "two"))
		require.Error(t, fetch(s2.URL, "one"))
		require.ErrorContains(t, fetch(s1.URL+"/redirect", "one"), "307")
	}
	require.Zero(t, reached.Load())
}

func TestScrapeTLSScraperEndToEnd(t *testing.T) {
	profile, pair := scrapeTLSFixture(t)
	srv := startScrapeTLSServer(t, pair, func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "synthetic-user" || p != "synthetic-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "keep_metric{source=\"synthetic\"} 42\ndrop_metric 99\n")
	})
	client, err := NewMetricsClient(ClientOpts{ScrapeTLSProfiles: map[string]scrapetls.Profile{"exporter": profile}})
	require.NoError(t, err)
	defer client.Close()
	output := &capturingClient{}
	s := NewScraper(&ScraperOpts{
		RemoteClients:  []remote.RemoteWriteClient{output},
		AddLabels:      map[string]string{"fleet": "test"},
		DropMetrics:    []*regexp.Regexp{regexp.MustCompile("^drop_metric$")},
		MaxBatchSize:   100,
		ScrapeInterval: time.Minute,
	})
	s.scrapeClient = client
	s.scrapeTarget(context.Background(), ScrapeTarget{
		Static: true, Addr: srv.URL, TLSProfile: "exporter", Namespace: "metrics", Pod: "exporter", Container: "sampler",
	})
	require.Equal(t, []map[string]string{{
		"__name__": "keep_metric", "source": "synthetic", "fleet": "test",
		"adxmon_namespace": "metrics", "adxmon_pod": "exporter", "adxmon_container": "sampler",
	}}, output.seriesLabels)
}

func TestScrapeTLSDiscovery(t *testing.T) {
	allowed := scrapetls.Profile{AllowedNamespaces: []string{"metrics"}}
	s := NewScraper(&ScraperOpts{ScrapeTLSProfiles: map[string]scrapetls.Profile{"a": allowed, "b": allowed, "static-only": {}}})
	p := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: "pod-uid", Namespace: "metrics", Name: "exporter", Annotations: map[string]string{
			"adx-mon/scrape": "true", "adx-mon/scheme": "https", "adx-mon/tls-profile": "a",
		}},
		Spec:   v1.PodSpec{Containers: []v1.Container{{Name: "exporter", Ports: []v1.ContainerPort{{ContainerPort: 9000}, {ContainerPort: 9001}}}}},
		Status: v1.PodStatus{PodIP: "127.0.0.1"},
	}
	requireProfile := func(want string, n int) {
		t.Helper()
		targets := s.Targets()
		require.Len(t, targets, n)
		for _, target := range targets {
			require.Equal(t, want, target.TLSProfile)
		}
	}

	s.OnAdd(p, false)
	requireProfile("a", 2)

	// Changing the selected profile replaces the targets.
	p2 := p.DeepCopy()
	p2.Annotations["adx-mon/tls-profile"] = "b"
	s.OnUpdate(p, p2)
	requireProfile("b", 2)

	// An unknown profile, a profile the namespace may not use, or an HTTP target removes the pod's targets rather
	// than scraping it without the profile.
	for _, mutate := range []func(*v1.Pod){
		func(p *v1.Pod) { p.Annotations["adx-mon/tls-profile"] = "unknown" },
		func(p *v1.Pod) { p.Annotations["adx-mon/tls-profile"] = "static-only" },
		func(p *v1.Pod) { p.Annotations["adx-mon/scheme"] = "http" },
	} {
		s.OnAdd(p, false)
		requireProfile("a", 2)
		bad := p.DeepCopy()
		mutate(bad)
		s.OnUpdate(p, bad)
		require.Empty(t, s.Targets())
	}
	untrusted := p.DeepCopy()
	untrusted.UID, untrusted.Namespace = "other-uid", "untrusted"
	s.OnAdd(untrusted, false)
	require.Empty(t, s.Targets())

	// All target constructors, including named target lists and single ports, carry the profile.
	p2 = p.DeepCopy()
	p2.Annotations["adx-mon/targets"] = "/one:9000,/two:9001"
	s.OnAdd(p2, false)
	requireProfile("a", 2)
	p3 := p2.DeepCopy()
	p3.Annotations["adx-mon/targets"] = "/one:9000"
	s.OnUpdate(p2, p3)
	requireProfile("a", 1)
	p4 := p3.DeepCopy()
	delete(p4.Annotations, "adx-mon/targets")
	p4.Annotations["adx-mon/port"] = "9001"
	s.OnUpdate(p3, p4)
	requireProfile("a", 1)
	require.Contains(t, s.Targets()[0].Addr, ":9001/")
	s.OnDelete(p4)
	require.Empty(t, s.Targets())

	// Pods without the annotation are unchanged.
	delete(p.Annotations, "adx-mon/tls-profile")
	delete(p.Annotations, "adx-mon/scheme")
	s.OnAdd(p, false)
	requireProfile("", 2)
	for _, target := range s.Targets() {
		require.True(t, strings.HasPrefix(target.Addr, "http://"))
	}
}

func TestScrapeTLSStaticTargets(t *testing.T) {
	profile, _ := scrapeTLSFixture(t)
	for _, tc := range []struct {
		name, addr, profile string
		missingFiles        bool
		wantError           bool
	}{
		{"profiled", "https://exporter.test/metrics", "secure", false, false},
		{"unknown-profile", "https://exporter.test/metrics", "unknown", false, true},
		{"http", "http://exporter.test/metrics", "secure", false, true},
		{"missing-files-start-unavailable", "https://exporter.test/metrics", "secure", true, false},
		{"default", "http://exporter.test/metrics", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := profile
			if tc.missingFiles {
				p.UsernameFile += ".missing"
			}
			s := NewScraper(&ScraperOpts{
				DisableDiscovery:  true,
				ScrapeInterval:    time.Hour,
				HealthChecker:     &scrapeTestHealth{},
				ScrapeTLSProfiles: map[string]scrapetls.Profile{"secure": p},
				Targets: []ScrapeTarget{{
					Static: true, Addr: tc.addr, TLSProfile: tc.profile, Namespace: "labels-only", Pod: "static", Container: "exporter",
				}},
			})
			err := s.Open(context.Background())
			if tc.wantError {
				require.Error(t, err)
				require.NoError(t, s.Close())
				return
			}
			require.NoError(t, err)
			require.Len(t, s.Targets(), 1)
			require.Equal(t, tc.profile, s.Targets()[0].TLSProfile)
			require.NoError(t, s.Close())
			require.NoError(t, s.Close())
		})
	}
}

func scrapeTLSInfoUsernames(t *testing.T, profile string) []string {
	t.Helper()
	ch := make(chan prometheus.Metric, 64)
	metrics.CollectorScrapeTLSProfileInfo.Collect(ch)
	close(ch)
	var usernames []string
	for m := range ch {
		var out dto.Metric
		require.NoError(t, m.Write(&out))
		labels := map[string]string{}
		for _, l := range out.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		require.NotContains(t, labels, "password")
		if labels["profile"] == profile {
			require.Equal(t, float64(1), out.GetGauge().GetValue())
			usernames = append(usernames, labels["username"])
		}
	}
	slices.Sort(usernames)
	return usernames
}

func TestScrapeTLSUnavailableProfileFailsClosedAndRecovers(t *testing.T) {
	const name = "unavailable-exporter"
	profile, pair := scrapeTLSFixture(t)
	var requests atomic.Int64
	srv := startScrapeTLSServer(t, pair, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		u, p, ok := r.BasicAuth()
		if !ok || !strings.HasPrefix(u, "synthetic-user") || p != "synthetic-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "keep_metric 1\n")
	})
	caPEM, err := os.ReadFile(profile.CAFile)
	require.NoError(t, err)
	restore := func() {
		require.NoError(t, os.WriteFile(profile.CAFile, caPEM, 0600))
		require.NoError(t, os.WriteFile(profile.UsernameFile, []byte("synthetic-user"), 0600))
		require.NoError(t, os.WriteFile(profile.PasswordFile, []byte("synthetic-password"), 0600))
	}
	for _, path := range []string{profile.CAFile, profile.UsernameFile, profile.PasswordFile} {
		require.NoError(t, os.Remove(path))
	}

	// Missing files at startup do not fail the client.
	c, err := NewMetricsClient(ClientOpts{
		ScrapeTLSProfiles:        map[string]scrapetls.Profile{name: profile},
		ScrapeTLSRefreshInterval: time.Hour,
	})
	require.NoError(t, err)
	defer c.Close()
	// A trusted default client and bearer token must never be used instead.
	useTrustedDefaultClient(c, srv, profile.ServerName, "synthetic-service-account-token")
	output := &capturingClient{}
	s := NewScraper(&ScraperOpts{RemoteClients: []remote.RemoteWriteClient{output}, MaxBatchSize: 100, ScrapeInterval: time.Minute})
	s.scrapeClient = c
	target := ScrapeTarget{Static: true, Addr: srv.URL, TLSProfile: name, Namespace: "metrics", Pod: "exporter", Container: "sampler"}
	failClosed := func() {
		t.Helper()
		_, err := c.FetchMetricsWithProfile(srv.URL, name)
		require.ErrorContains(t, err, "unavailable")
		_, err = c.FetchMetricsIteratorWithProfile(context.Background(), srv.URL, name)
		require.ErrorContains(t, err, "unavailable")
		output.seriesLabels = nil
		s.scrapeTarget(context.Background(), target)
		require.Empty(t, output.seriesLabels)
		require.Zero(t, requests.Load(), "unavailable profile must not send any request")
		require.Empty(t, scrapeTLSInfoUsernames(t, name))
	}
	failClosed()

	// Files that appear later are adopted on the next retry.
	restore()
	failClosed()
	c.profilesMu.Lock()
	c.profiles[name].checked = time.Time{}
	c.profilesMu.Unlock()
	_, err = c.FetchMetricsWithProfile(srv.URL, name)
	require.NoError(t, err)
	require.Equal(t, int64(1), requests.Load())
	require.Equal(t, []string{"synthetic-user"}, scrapeTLSInfoUsernames(t, name))

	// Rechecks follow username changes and drop the old series.
	c.opts.ScrapeTLSRefreshInterval = time.Nanosecond
	require.NoError(t, os.WriteFile(profile.UsernameFile, []byte("synthetic-user-g2\n"), 0600))
	output.seriesLabels = nil
	s.scrapeTarget(context.Background(), target)
	require.Len(t, output.seriesLabels, 1)
	require.Equal(t, []string{"synthetic-user-g2"}, scrapeTLSInfoUsernames(t, name))

	// Files that disappear later make the profile unavailable again.
	for _, path := range []string{profile.CAFile, profile.UsernameFile, profile.PasswordFile} {
		restore()
		require.NoError(t, os.Remove(path))
		requests.Store(0)
		failClosed()
		restore()
		_, err = c.FetchMetricsWithProfile(srv.URL, name)
		require.NoError(t, err)
		require.Equal(t, []string{"synthetic-user"}, scrapeTLSInfoUsernames(t, name))
	}
	require.NoError(t, c.Close())
	require.Empty(t, scrapeTLSInfoUsernames(t, name))
}
