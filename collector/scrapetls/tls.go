// Package scrapetls builds HTTP clients that scrape Prometheus exporters with
// HTTP Basic authentication over verified TLS.
//
// A Profile names a CA bundle and a pair of credential files. The files are
// read and validated on every request, so rotated credentials and CA bundles
// are picked up without a restart.
package scrapetls

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
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

// readCA reads the CA bundle once and checks that it holds at least one PEM
// certificate. An empty bundle is rejected rather than falling back to the
// system roots.
func (p Profile) readCA(ctx context.Context) ([]byte, error) {
	ca, err := promconfig.NewFileSecret(p.CAFile).Fetch(ctx)
	if err != nil {
		return nil, &FileError{fmt.Errorf("CA file: %w", err)}
	}
	if !x509.NewCertPool().AppendCertsFromPEM([]byte(ca)) {
		return nil, &FileError{errors.New("CA file holds no PEM certificates")}
	}
	return []byte(ca), nil
}

// newCAClient returns a Prometheus HTTP client that trusts exactly ca. The
// CA is passed inline so the client never re-reads the file; Transport
// detects rotation by comparing the same bytes it built the client from.
func (p Profile) newCAClient(ca []byte, dialTimeout time.Duration) (*http.Client, error) {
	return promconfig.NewClientFromConfig(promconfig.HTTPClientConfig{
		TLSConfig: promconfig.TLSConfig{
			CA:         string(ca),
			ServerName: p.ServerName,
			MinVersion: promconfig.TLSVersion(tls.VersionTLS12),
		},
		FollowRedirects: false,
	}, "adx-mon-collector", promconfig.WithDialContextFunc((&net.Dialer{Timeout: dialTimeout}).DialContext))
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

// FileError reports profile files that cannot be read or do not hold usable
// Basic auth credentials or CA certificates. It describes a local problem
// with the profile, not with a target, and the request was not sent.
type FileError struct {
	Err error
}

func (e *FileError) Error() string { return "scrape TLS profile files: " + e.Err.Error() }

func (e *FileError) Unwrap() error { return e.Err }

// readCredentials reads and validates the Basic auth credentials. Surrounding
// whitespace is trimmed. The username must be nonempty valid UTF-8 without
// ':' and the password must be nonempty.
func (p Profile) readCredentials(ctx context.Context) (string, string, error) {
	username, err := promconfig.NewFileSecret(p.UsernameFile).Fetch(ctx)
	if err != nil {
		return "", "", &FileError{fmt.Errorf("username file: %w", err)}
	}
	if username == "" || strings.Contains(username, ":") || !utf8.ValidString(username) {
		return "", "", &FileError{errors.New("username file is empty, contains ':' or is not valid UTF-8")}
	}
	password, err := promconfig.NewFileSecret(p.PasswordFile).Fetch(ctx)
	if err != nil {
		return "", "", &FileError{fmt.Errorf("password file: %w", err)}
	}
	if password == "" {
		return "", "", &FileError{errors.New("password file is empty")}
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
	if _, err := p.readCA(context.Background()); err != nil {
		return "", err
	}
	return username, nil
}

// Transport rejects requests that do not satisfy ValidateTarget and replaces
// caller-supplied credentials with the profile's. It re-reads the profile
// files on every request and returns a *FileError without sending the request
// if they are unusable. When the CA bundle changes, the connection pool is
// replaced with one built from that same snapshot. TLS verification is
// delegated to the Prometheus HTTP client, so a target that fails it returns
// an ordinary error.
type Transport struct {
	profile     Profile
	dialTimeout time.Duration

	mu     sync.Mutex
	caData []byte
	client *http.Client
	closed bool
}

// testHookCALoaded is called after NewClient reads the CA bundle.
var testHookCALoaded = func() {}

// NewClient returns an HTTP client for p. Its Transport is a *Transport.
// Redirects are not followed and proxies from the environment are ignored.
func NewClient(p Profile, dialTimeout time.Duration) (*http.Client, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if _, _, err := p.readCredentials(context.Background()); err != nil {
		return nil, err
	}
	// The trusted roots and the data later compared to detect rotation come
	// from this single read, so a rotation during construction is seen as a
	// change by the next request.
	ca, err := p.readCA(context.Background())
	if err != nil {
		return nil, err
	}
	testHookCALoaded()
	inner, err := p.newCAClient(ca, dialTimeout)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport:     &Transport{profile: p, dialTimeout: dialTimeout, caData: ca, client: inner},
		CheckRedirect: inner.CheckRedirect,
	}, nil
}

// clientFor returns the client that trusts ca, replacing the current one if
// the bundle changed.
func (t *Transport) clientFor(ca []byte) (*http.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("scrape TLS transport closed")
	}
	if bytes.Equal(ca, t.caData) {
		return t.client, nil
	}
	client, err := t.profile.newCAClient(ca, t.dialTimeout)
	if err != nil {
		return nil, &FileError{fmt.Errorf("CA file: %w", err)}
	}
	t.client.CloseIdleConnections()
	t.caData, t.client = ca, client
	return client, nil
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := ValidateTarget(req.URL.String()); err != nil {
		return nil, err
	}
	username, password, err := t.profile.readCredentials(req.Context())
	if err != nil {
		return nil, err
	}
	ca, err := t.profile.readCA(req.Context())
	if err != nil {
		return nil, err
	}
	client, err := t.clientFor(ca)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Del("Proxy-Authorization")
	clone.SetBasicAuth(username, password)
	return client.Transport.RoundTrip(clone)
}

func (t *Transport) CloseIdleConnections() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.client.CloseIdleConnections()
}

// Close rejects further requests and closes idle connections.
func (t *Transport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	t.client.CloseIdleConnections()
}
