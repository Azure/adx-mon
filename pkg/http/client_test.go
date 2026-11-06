package http

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTLSProtoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Proto))
	}))
	srv.EnableHTTP2 = true
	// httptest advertises only "h2" when EnableHTTP2 is set; offer both so HTTP/1-only clients can connect.
	srv.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestNewClientProtocols(t *testing.T) {
	srv := newTLSProtoServer(t)

	tests := []struct {
		name         string
		disableHTTP2 bool
		wantProto    string
	}{
		{"http2 negotiated by default", false, "HTTP/2.0"},
		{"http1 only when http2 disabled", true, "HTTP/1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(ClientOpts{InsecureSkipVerify: true, DisableHTTP2: tt.disableHTTP2})
			resp, err := client.Get(srv.URL)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tt.wantProto, resp.Proto)
		})
	}
}

func TestNewClientHTTP2HealthCheck(t *testing.T) {
	transport := NewClient(ClientOpts{}).Transport.(*http.Transport)
	require.NotNil(t, transport.HTTP2)
	require.NotZero(t, transport.HTTP2.SendPingTimeout)
	require.NotZero(t, transport.HTTP2.PingTimeout)
}
