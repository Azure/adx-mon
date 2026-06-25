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

	tests := map[string]string{
		"/metrics":             "/metrics",
		"/debug/pprof/":        "/debug/pprof/",
		"/debug/pprof/cmdline": "/debug/pprof/cmdline",
		"/debug/pprof/profile": "/debug/pprof/profile",
		"/debug/pprof/symbol":  "/debug/pprof/symbol",
		"/debug/pprof/trace":   "/debug/pprof/trace",
	}
	for path, wantPattern := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		_, pattern := mux.Handler(req)

		require.Equal(t, wantPattern, pattern)
	}
}

func TestHTTPMuxRegistersAlertHandler(t *testing.T) {
	svc := &Alerter{
		alertHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	}
	req := httptest.NewRequest(http.MethodPost, "/alerts", nil)
	rec := httptest.NewRecorder()

	svc.httpMux().ServeHTTP(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
}
