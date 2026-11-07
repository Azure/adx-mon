// Package scrapetls builds HTTP clients that scrape Prometheus exporters with
// HTTP Basic authentication over verified TLS.
//
// A Profile names a CA bundle and a pair of credential files. The files are
// re-read by the underlying Prometheus HTTP client on every request, so
// rotated credentials and CA bundles are picked up without a restart.
package scrapetls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	promconfig "github.com/prometheus/common/config"
)

// Profile is a named exporter identity: a trusted CA and Basic auth
// credentials read from files.
type Profile struct {
	CAFile            string   `toml:"ca-file" comment:"Path to the PEM CA bundle used to verify the exporter's serving certificate."`
	ServerName        string   `toml:"server-name,omitempty" comment:"Optional TLS server name to verify instead of the host in the target URL."`
	UsernameFile      string   `toml:"username-file" comment:"Path to a file containing the Basic auth username."`
	PasswordFile      string   `toml:"password-file" comment:"Path to a file containing the Basic auth password."`
	AllowedNamespaces []string `toml:"allowed-namespaces,omitempty" comment:"Namespaces whose pods may select this profile with the adx-mon/tls-profile annotation.  Empty denies all discovered pods; static targets may still use the profile."`
}

// AllowsNamespace reports whether discovered pods in namespace may use the profile.
func (p Profile) AllowsNamespace(namespace string) bool {
	return namespace != "" && slices.Contains(p.AllowedNamespaces, namespace)
}

// ValidateTarget reports whether target may be scraped with a profile.
// Profiled targets must use HTTPS and must not embed user credentials.
func ValidateTarget(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("scrape TLS requires an HTTPS URL without user credentials")
	}
	return nil
}

func (p Profile) tlsConfig() promconfig.TLSConfig {
	return promconfig.TLSConfig{
		CAFile:     p.CAFile,
		ServerName: p.ServerName,
		MinVersion: promconfig.TLSVersion(tls.VersionTLS12),
	}
}

// Validate checks only the structure of the profile. Missing or invalid files
// are reported by CheckFiles so a collector can keep running while a profile
// is unavailable.
func (p Profile) Validate() error {
	if p.CAFile == "" || p.UsernameFile == "" || p.PasswordFile == "" {
		return errors.New("ca-file, username-file and password-file must be set")
	}
	return nil
}

// CheckFiles reads the credential files and parses the CA bundle. It returns
// the username, which is not secret; the password is never returned.
func (p Profile) CheckFiles() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	// Use the same trimming semantics as the per-request reader.
	username, err := promconfig.NewFileSecret(p.UsernameFile).Fetch(context.Background())
	if err != nil {
		return "", fmt.Errorf("username file: %w", err)
	}
	if username == "" || strings.Contains(username, ":") {
		return "", errors.New("username file is empty or contains ':'")
	}
	password, err := promconfig.NewFileSecret(p.PasswordFile).Fetch(context.Background())
	if err != nil {
		return "", fmt.Errorf("password file: %w", err)
	}
	if password == "" {
		return "", errors.New("password file is empty")
	}
	cfg := p.tlsConfig()
	if _, err := promconfig.NewTLSConfig(&cfg); err != nil {
		return "", err
	}
	return username, nil
}

// Transport rejects requests that do not satisfy ValidateTarget and strips
// caller-supplied credentials. Credential loading, change detection, TLS
// verification and connection-pool replacement are delegated to the
// Prometheus HTTP client.
type Transport struct {
	client *http.Client
	closed atomic.Bool
}

// NewClient returns an HTTP client for p. Its Transport is a *Transport.
// Redirects are not followed and proxies from the environment are ignored.
func NewClient(p Profile, dialTimeout time.Duration) (*http.Client, error) {
	if _, err := p.CheckFiles(); err != nil {
		return nil, err
	}
	client, err := promconfig.NewClientFromConfig(promconfig.HTTPClientConfig{
		BasicAuth:       &promconfig.BasicAuth{UsernameFile: p.UsernameFile, PasswordFile: p.PasswordFile},
		TLSConfig:       p.tlsConfig(),
		FollowRedirects: false,
	}, "adx-mon-collector", promconfig.WithDialContextFunc((&net.Dialer{Timeout: dialTimeout}).DialContext))
	if err != nil {
		return nil, err
	}
	inner := *client
	client.Transport = &Transport{client: &inner}
	return client, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.closed.Load() {
		return nil, errors.New("scrape TLS transport closed")
	}
	if err := ValidateTarget(req.URL.String()); err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Del("Authorization")
	clone.Header.Del("Proxy-Authorization")
	return t.client.Transport.RoundTrip(clone)
}

func (t *Transport) CloseIdleConnections() {
	t.client.CloseIdleConnections()
}

// Close rejects further requests and closes idle connections.
func (t *Transport) Close() {
	t.closed.Store(true)
	t.CloseIdleConnections()
}
