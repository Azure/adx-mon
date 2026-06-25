package alerter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPMuxRegistersMetricsAndPprof(t *testing.T) {
	svc := &Alerter{}
	mux := svc.httpMux()

	for _, path := range []string{"/metrics", "/debug/pprof/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	}
}
