package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

func TestNewHttpServer_Endpoints(t *testing.T) {
	h := NewServer(&ServerOpts{
		ListenAddr: "localhost:0",
	})
	h.RegisterHandler("/metrics", promhttp.Handler())
	require.NoError(t, h.Open(context.Background()))
	defer h.Close()

	srv := httptest.NewServer(h.mux)
	defer srv.Close()

	tests := []struct {
		endpoint string
		status   int
	}{
		{"/metrics", http.StatusOK},
	}

	for _, tt := range tests {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", srv.URL, tt.endpoint), nil)
		require.NoError(t, err)
		resp, err := srv.Client().Do(req)

		require.NoError(t, err)
		require.Equal(t, tt.status, resp.StatusCode, tt.endpoint)
	}
}

func TestHttpServerProtocols(t *testing.T) {
	h := NewServer(&ServerOpts{ListenAddr: "localhost:0"})
	h.RegisterHandlerFunc("/proto", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Proto))
	})
	require.NoError(t, h.Open(t.Context()))
	defer h.Close()
	url := "http://" + h.listener.Addr().String() + "/proto"

	h2c := new(http.Protocols)
	h2c.SetUnencryptedHTTP2(true)
	http1 := new(http.Protocols)
	http1.SetHTTP1(true)

	tests := []struct {
		name      string
		protocols *http.Protocols
		wantProto string
	}{
		{"unencrypted http2 with prior knowledge", h2c, "HTTP/2.0"},
		{"http1", http1, "HTTP/1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: &http.Transport{Protocols: tt.protocols}}
			resp, err := client.Get(url)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, tt.wantProto, resp.Proto)
			require.Equal(t, tt.wantProto, string(body))
		})
	}
}

func TestHttpHandlerWithTimeout(t *testing.T) {
	contextCanceled := make(chan struct{})
	handler := (&HttpHandler{
		Handler: func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			close(contextCanceled)
		},
		Timeout: 10 * time.Millisecond,
	}).WithTimeout()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/logs", nil))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	select {
	case <-contextCanceled:
	case <-time.After(time.Second):
		t.Fatal("handler context was not canceled after timeout")
	}
}
