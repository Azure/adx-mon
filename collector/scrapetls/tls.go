// Package scrapetls builds HTTP clients that scrape Prometheus exporters with
// HTTP Basic authentication over verified TLS.
//
// A Profile names a CA bundle and a pair of credential files. The credential
// files are read and validated on every request, and the CA bundle is
// re-read by the underlying Prometheus HTTP client, so rotated credentials
// and CA bundles are picked up without a restart.
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
	"unicode/utf8"

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

// CredentialError reports credential files that cannot be read or do not hold
// usable Basic auth credentials. The request was not sent.
type CredentialError struct {
	Err error
}

func (e *CredentialError) Error() string { return "scrape TLS credentials: " + e.Err.Error() }

func (e *CredentialError) Unwrap() error { return e.Err }

// readCredentials reads and validates the Basic auth credentials. Surrounding
// whitespace is trimmed. The username must be nonempty valid UTF-8 without
// ':' and the password must be nonempty.
func (p Profile) readCredentials(ctx context.Context) (string, string, error) {
	username, err := promconfig.NewFileSecret(p.UsernameFile).Fetch(ctx)
	if err != nil {
		return "", "", &CredentialError{fmt.Errorf("username file: %w", err)}
	}
	if username == "" || strings.Contains(username, ":") || !utf8.ValidString(username) {
		return "", "", &CredentialError{errors.New("username file is empty, contains ':' or is not valid UTF-8")}
	}
	password, err := promconfig.NewFileSecret(p.PasswordFile).Fetch(ctx)
	if err != nil {
		return "", "", &CredentialError{fmt.Errorf("password file: %w", err)}
	}
	if password == "" {
		return "", "", &CredentialError{errors.New("password file is empty")}
	}
	return username, password, nil
}

// CheckFiles reads the credential files and parses the CA bundle. It returns
// the username, which is not secret; the password is never returned.
func (p Profile) CheckFiles() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	username, _, err := p.readCredentials(context.Background())
	if err != nil {
		return "", err
	}
	cfg := p.tlsConfig()
	if _, err := promconfig.NewTLSConfig(&cfg); err != nil {
		return "", err
	}
	return username, nil
}

// Transport rejects requests that do not satisfy ValidateTarget, replaces
// caller-supplied credentials with the profile's, and returns a
// *CredentialError without sending the request if they are unusable. CA
// loading, TLS verification and connection-pool replacement are delegated to
// the Prometheus HTTP client.
type Transport struct {
	profile Profile
	client  *http.Client
	closed  atomic.Bool
}

// NewClient returns an HTTP client for p. Its Transport is a *Transport.
// Redirects are not followed and proxies from the environment are ignored.
func NewClient(p Profile, dialTimeout time.Duration) (*http.Client, error) {
	if _, err := p.CheckFiles(); err != nil {
		return nil, err
	}
	client, err := promconfig.NewClientFromConfig(promconfig.HTTPClientConfig{
		TLSConfig:       p.tlsConfig(),
		FollowRedirects: false,
	}, "adx-mon-collector", promconfig.WithDialContextFunc((&net.Dialer{Timeout: dialTimeout}).DialContext))
	if err != nil {
		return nil, err
	}
	inner := *client
	client.Transport = &Transport{profile: p, client: &inner}
	return client, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.closed.Load() {
		return nil, errors.New("scrape TLS transport closed")
	}
	if err := ValidateTarget(req.URL.String()); err != nil {
		return nil, err
	}
	username, password, err := t.profile.readCredentials(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Del("Proxy-Authorization")
	clone.SetBasicAuth(username, password)
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
