package scrapetls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newCA(t *testing.T) testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "synthetic CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return testCA{cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca testCA) leaf(t *testing.T, name string, expired bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if expired {
		template.NotAfter = time.Now().Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, err)
	return pair
}

func profileFiles(t *testing.T, ca, username, password []byte) Profile {
	t.Helper()
	dir := t.TempDir()
	p := Profile{
		CAFile:       filepath.Join(dir, "ca.pem"),
		UsernameFile: filepath.Join(dir, "username"),
		PasswordFile: filepath.Join(dir, "password"),
		ServerName:   "exporter.test",
	}
	writeFile(t, p.CAFile, ca)
	writeFile(t, p.UsernameFile, username)
	writeFile(t, p.PasswordFile, password)
	return p
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, data, 0600))
}

func tlsServer(t *testing.T, ca testCA, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{ca.leaf(t, "exporter.test", false)}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func secureClient(t *testing.T, p Profile) (*http.Client, *Transport) {
	t.Helper()
	client, err := NewClient(p, time.Second)
	require.NoError(t, err)
	rt := client.Transport.(*Transport)
	t.Cleanup(rt.Close)
	client.Timeout = 3 * time.Second
	return client, rt
}

func get(t *testing.T, c *http.Client, target string, wantError bool) string {
	t.Helper()
	resp, err := c.Get(target)
	if wantError {
		if resp != nil {
			resp.Body.Close()
		}
		require.Error(t, err)
		return ""
	}
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func TestAuthentication(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "client" || password != "synthetic-password" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("wrong Basic auth or proxy authorization leaked")
		}
		if len(r.TLS.PeerCertificates) != 0 {
			t.Error("unexpected client certificate")
		}
		fmt.Fprint(w, username)
	}))
	for _, tc := range []struct {
		name       string
		serverName string
		serverCA   []byte
		fail       bool
	}{
		{"valid", "exporter.test", ca.pem, false},
		{"url-identity", "", ca.pem, false},
		{"wrong-server-ca", "exporter.test", other.pem, true},
		{"wrong-server-name", "wrong.test", ca.pem, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := profileFiles(t, tc.serverCA, []byte("client"), []byte("synthetic-password"))
			p.ServerName = tc.serverName
			c, _ := secureClient(t, p)
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer must-not-leak")
			req.Header.Set("Proxy-Authorization", "Basic must-not-leak")
			resp, err := c.Do(req)
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			require.Equal(t, tc.fail, err != nil, "error: %v", err)
		})
	}
	t.Run("expired-server", func(t *testing.T) {
		expired := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("accepted expired server") }))
		expired.TLS = &tls.Config{Certificates: []tls.Certificate{ca.leaf(t, "exporter.test", true)}}
		expired.StartTLS()
		defer expired.Close()
		c, _ := secureClient(t, profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password")))
		get(t, c, expired.URL, true)
	})
}

func TestInitialFiles(t *testing.T) {
	ca := newCA(t)
	for _, tc := range []string{
		"missing-ca", "missing-username", "missing-password", "bad-ca", "empty-username", "empty-password",
		"colon-username", "invalid-utf8-username", "unreadable-username", "unreadable-password", "unset-ca", "unset-username",
		"unset-password",
	} {
		t.Run(tc, func(t *testing.T) {
			p := profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password"))
			switch tc {
			case "missing-ca":
				require.NoError(t, os.Remove(p.CAFile))
			case "missing-username":
				require.NoError(t, os.Remove(p.UsernameFile))
			case "missing-password":
				require.NoError(t, os.Remove(p.PasswordFile))
			case "bad-ca":
				writeFile(t, p.CAFile, []byte("not a CA"))
			case "empty-username":
				writeFile(t, p.UsernameFile, []byte(" \n"))
			case "empty-password":
				writeFile(t, p.PasswordFile, nil)
			case "colon-username":
				writeFile(t, p.UsernameFile, []byte("unusable:name"))
			case "invalid-utf8-username":
				writeFile(t, p.UsernameFile, []byte("client\xff"))
			case "unreadable-username":
				p.UsernameFile = filepath.Dir(p.UsernameFile)
			case "unreadable-password":
				p.PasswordFile = filepath.Dir(p.PasswordFile)
			case "unset-ca":
				p.CAFile = ""
			case "unset-username":
				p.UsernameFile = ""
			case "unset-password":
				p.PasswordFile = ""
			}
			_, err := NewClient(p, time.Second)
			require.Error(t, err)
			_, err = p.CheckFiles()
			require.Error(t, err)
			// Only unset paths are structural errors; file problems affect availability.
			require.Equal(t, strings.HasPrefix(tc, "unset-"), p.Validate() != nil)
		})
	}
}

func TestLibraryFileSemantics(t *testing.T) {
	ca := newCA(t)
	p := profileFiles(t, ca.pem, []byte(" client \n"), []byte(" synthetic-password \r\n"))
	cwd, err := os.Getwd()
	require.NoError(t, err)
	for _, path := range []*string{&p.CAFile, &p.UsernameFile, &p.PasswordFile} {
		*path, err = filepath.Rel(cwd, *path)
		require.NoError(t, err)
	}
	// Use the library's standard PEM parser and relative paths rather than
	// imposing a stricter policy.
	writeFile(t, p.CAFile, append(append([]byte{}, ca.pem...), []byte("\ntrailing data")...))
	username, err := p.CheckFiles()
	require.NoError(t, err)
	require.Equal(t, "client", username)
	c, _ := secureClient(t, p)
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "client" || p != "synthetic-password" {
			t.Error("library file trimming changed")
		}
		fmt.Fprint(w, "ok")
	}))
	get(t, c, srv.URL, false)
}

func TestUpdatesAndKeepalive(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	var connections atomic.Int64
	var addresses sync.Map
	// The remote address is stable only when the same keepalive connection is used.
	var requests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if _, loaded := addresses.LoadOrStore(r.RemoteAddr, true); !loaded {
			connections.Add(1)
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != "client" || (p != "first" && p != "second") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, p)
	})
	pair := ca.leaf(t, "exporter.test", false)
	var serving atomic.Pointer[tls.Certificate]
	serving.Store(&pair)
	srv := httptest.NewUnstartedServer(handler)
	srv.TLS = &tls.Config{
		Certificates:   []tls.Certificate{pair},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return serving.Load(), nil },
	}
	srv.StartTLS()
	defer srv.Close()
	p := profileFiles(t, ca.pem, []byte("client"), []byte("first"))
	c, _ := secureClient(t, p)
	require.Equal(t, "first", get(t, c, srv.URL, false))
	get(t, c, srv.URL, false)
	require.Equal(t, int64(1), connections.Load(), "keepalive not exercised")

	replacement := p.PasswordFile + ".next"
	writeFile(t, replacement, []byte("second"))
	require.NoError(t, os.Rename(replacement, p.PasswordFile))
	require.Equal(t, "second", get(t, c, srv.URL, false), "stale password")
	require.Equal(t, int64(1), connections.Load(), "password rotation unnecessarily replaced keepalive connection")

	for _, tc := range []string{
		"missing-ca", "missing-username", "missing-password", "bad-ca", "empty-username", "empty-password",
		"colon-username", "invalid-utf8-username", "wrong-password", "unreadable-username", "unreadable-password",
	} {
		t.Run(tc, func(t *testing.T) {
			switch tc {
			case "missing-ca":
				require.NoError(t, os.Remove(p.CAFile))
			case "missing-username":
				require.NoError(t, os.Remove(p.UsernameFile))
			case "missing-password":
				require.NoError(t, os.Remove(p.PasswordFile))
			case "bad-ca":
				writeFile(t, p.CAFile, []byte("bad"))
			case "empty-username":
				writeFile(t, p.UsernameFile, []byte(" \n"))
			case "empty-password":
				writeFile(t, p.PasswordFile, nil)
			case "colon-username":
				writeFile(t, p.UsernameFile, []byte("unusable:name"))
			case "invalid-utf8-username":
				writeFile(t, p.UsernameFile, []byte("client\xff"))
			case "wrong-password":
				writeFile(t, p.PasswordFile, []byte("wrong"))
			case "unreadable-username":
				require.NoError(t, os.Remove(p.UsernameFile))
				require.NoError(t, os.Mkdir(p.UsernameFile, 0700))
			case "unreadable-password":
				require.NoError(t, os.Remove(p.PasswordFile))
				require.NoError(t, os.Mkdir(p.PasswordFile, 0700))
			}
			before := requests.Load()
			switch tc {
			case "wrong-password":
				resp, err := c.Get(srv.URL)
				require.NoError(t, err)
				resp.Body.Close()
				require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			default:
				// Unusable files are reported before any request is sent.
				_, err := c.Get(srv.URL)
				var fileErr *FileError
				require.ErrorAs(t, err, &fileErr)
				require.Equal(t, before, requests.Load(), "request sent with unusable profile files")
			}
			switch tc {
			case "unreadable-username":
				require.NoError(t, os.Remove(p.UsernameFile))
			case "unreadable-password":
				require.NoError(t, os.Remove(p.PasswordFile))
			}
			writeFile(t, p.CAFile, ca.pem)
			writeFile(t, p.UsernameFile, []byte("client"))
			writeFile(t, p.PasswordFile, []byte("second"))
			require.Equal(t, "second", get(t, c, srv.URL, false))
		})
	}

	// A CA update that no longer trusts the pooled peer must reject it. This
	// is a target verification failure, not a problem with the files.
	writeFile(t, p.CAFile, other.pem)
	_, err := c.Get(srv.URL)
	var fileErr *FileError
	require.Error(t, err)
	require.False(t, errors.As(err, &fileErr), "TLS verification failure reported as a file error")
	pair2 := other.leaf(t, "exporter.test", false)
	serving.Store(&pair2)
	// Same endpoint, replaced server CA, same Basic credentials.
	require.Equal(t, "second", get(t, c, srv.URL, false))
	// Restoring only the old CA must invalidate the new pooled peer.
	writeFile(t, p.CAFile, ca.pem)
	get(t, c, srv.URL, true)
}

func TestCARotationDuringNewClient(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	p := profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password"))
	testHookCALoaded = func() { writeFile(t, p.CAFile, other.pem) }
	c, _ := secureClient(t, p)
	testHookCALoaded = func() {}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	// The rotation is seen as a change: the new CA is trusted and the removed one is not.
	require.Equal(t, "ok", get(t, c, tlsServer(t, other, ok).URL, false))
	get(t, c, tlsServer(t, ca, ok).URL, true)
}

func TestPolicyAndIsolation(t *testing.T) {
	ca := newCA(t)
	p := profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password"))
	var redirected atomic.Int64
	destination := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	c, rt := secureClient(t, p)
	resp, err := c.Get(srv.URL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.Zero(t, redirected.Load(), "followed redirect")

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("sent secured HTTP request") }))
	defer httpSrv.Close()
	get(t, c, httpSrv.URL, true)
	get(t, c, "https://user:password@"+srv.Listener.Addr().String(), true)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("used environment proxy") }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	ok := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, valid := r.BasicAuth()
		if !valid || u != "client" || p != "synthetic-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	get(t, c, ok.URL, false)

	c2, _ := secureClient(t, profileFiles(t, ca.pem, []byte("isolated"), []byte("other-password")))
	resp, err = c2.Get(ok.URL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "profile credentials were not isolated")
	get(t, c, ok.URL, false)

	rt.Close()
	rt.Close()
	get(t, c, ok.URL, true)
}

func TestRequestsDoNotSerializeNetworkIO(t *testing.T) {
	ca := newCA(t)
	c, _ := secureClient(t, profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password")))
	slowEntered := make(chan struct{})
	fastEntered := make(chan struct{})
	releaseSlow := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseSlow) })
	defer release()
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(slowEntered)
			<-releaseSlow
		} else {
			close(fastEntered)
		}
		fmt.Fprint(w, "ok")
	}))
	done := make(chan error, 2)
	fetch := func(path string) {
		resp, err := c.Get(srv.URL + path)
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		done <- err
	}
	go fetch("/slow")
	select {
	case <-slowEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not reach server")
	}
	go fetch("/fast")
	select {
	case <-fastEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked response serialized another request sharing the profile")
	}
	release()
	for range 2 {
		require.NoError(t, <-done)
	}
}

func TestConcurrentRequestsAndClose(t *testing.T) {
	ca := newCA(t)
	c, rt := secureClient(t, profileFiles(t, ca.pem, []byte("client"), []byte("synthetic-password")))
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			resp, err := c.Get(srv.URL)
			if err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		})
	}
	rt.CloseIdleConnections()
	rt.Close()
	wg.Wait()
	get(t, c, srv.URL, true)
}

func TestConcurrentRotation(t *testing.T) {
	ca := newCA(t)
	p := profileFiles(t, ca.pem, []byte("client"), []byte("first"))
	c, _ := secureClient(t, p)
	srv := tlsServer(t, ca, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "client" || (p != "first" && p != "second") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, p)
	}))
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			for range 10 {
				resp, err := c.Get(srv.URL)
				// Files may be briefly inconsistent while being rewritten; errors are
				// allowed, but a successful response must use a known identity.
				if err != nil {
					continue
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusUnauthorized {
					continue
				}
				if err != nil || (string(b) != "first" && string(b) != "second") {
					t.Errorf("unexpected response %q: %v", b, err)
				}
			}
		})
	}
	for range 10 {
		writeFile(t, p.PasswordFile, []byte("second"))
		writeFile(t, p.PasswordFile, []byte("first"))
	}
	wg.Wait()
	writeFile(t, p.PasswordFile, []byte("second"))
	require.Equal(t, "second", get(t, c, srv.URL, false))
}
