package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewHttpServer_Endpoints(t *testing.T) {
	h := NewServer(&ServerOpts{
		ListenAddr: "localhost:0",
	})
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
