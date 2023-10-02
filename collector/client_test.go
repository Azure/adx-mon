package collector_test

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Azure/adx-mon/collector"
	"github.com/stretchr/testify/require"
)

func TestService_Client(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("Expected gzip Accept-Encoding, got %s", r.Header.Get("Accept-Encoding"))
		}

		fmt.Fprintf(w, `
# HELP go_gc_duration_seconds A summary of the GC invocation durations.
# TYPE go_gc_duration_seconds summary
go_gc_duration_seconds{quantile="0"} 0.000109003
go_gc_duration_seconds{quantile="0.25"} 0.000194402
go_gc_duration_seconds{quantile="0.5"} 0.000225802
go_gc_duration_seconds{quantile="0.75"} 0.000269203
go_gc_duration_seconds{quantile="1"} 0.025809583
go_gc_duration_seconds_sum 3.91367433
go_gc_duration_seconds_count 8452
`)
	}))
	defer svr.Close()

	client, err := collector.NewMetricsClient()
	require.NoError(t, err)
	defer client.Close()

	m, err := client.FetchMetrics(svr.URL)
	require.NoError(t, err)
	require.Equal(t, 1, len(m))
}

func TestService_Client_Gzip(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("Expected gzip Accept-Encoding, got %s", r.Header.Get("Accept-Encoding"))
		}

		w.Header().Set("Content-Encoding", "gzip")
		gw := gzip.NewWriter(w)

		fmt.Fprintf(gw, `
# HELP go_gc_duration_seconds A summary of the GC invocation durations.
# TYPE go_gc_duration_seconds summary
go_gc_duration_seconds{quantile="0"} 0.000109003
go_gc_duration_seconds{quantile="0.25"} 0.000194402
go_gc_duration_seconds{quantile="0.5"} 0.000225802
go_gc_duration_seconds{quantile="0.75"} 0.000269203
go_gc_duration_seconds{quantile="1"} 0.025809583
go_gc_duration_seconds_sum 3.91367433
go_gc_duration_seconds_count 8452
`)

		gw.Flush()
		gw.Close()

	}))
	defer svr.Close()

	client, err := collector.NewMetricsClient()
	require.NoError(t, err)
	defer client.Close()

	m, err := client.FetchMetrics(svr.URL)
	require.NoError(t, err)
	require.Equal(t, 1, len(m))
}
