package logs_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	cotlp "github.com/Azure/adx-mon/collector/otlp"
	isvc "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	iotlp "github.com/Azure/adx-mon/ingestor/otlp"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestOTLPLogsE2E(t *testing.T) {
	// E2E test for the "happy path" from the perspective of a logs producer, such
	// as fluentbit, as received by Collector and sent to Ingestor via the Proxy handler.
	//
	// We're asserting that our logs producer is able to send a valid OTLP logs payload
	// to Collector and receive a valid ExportLogsServiceResponse with status 200. We'll
	// then verify that Ingestor wrote the logs to storage and verify its contents. We're
	// going to stop short of testing that logs are written to Kusto because that would
	// be testing a shared code path that all handlers take, which will be left to a
	// separate integration test.

	// We're going to test both the Proxy and Transfer handlers, which are exposed by Collector.
	tests := []struct {
		URLPath string
	}{
		// {
		// 	URLPath: logsv1connect.LogsServiceExportProcedure,
		// },
		{
			URLPath: "/v1/logs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.URLPath, func(t *testing.T) {

			ctx, cancel := context.WithCancel(context.Background())
			ingestorDir := t.TempDir()

			// Create our Ingestor HTTP instance, which exposes Ingestor's OTLP logs handler. This
			// code path is identical to a real Ingestor instance at the point of OTLP logs ingestion.
			ingestorURL, done := NewIngestorHandler(t, ctx, ingestorDir)
			// Likewise, we create our Collector HTTP instance, which exposes Collector's OTLP logs
			// and is identical to the way in which a downstream component, such as fluentbit, would
			// interact with Collector. Here we pass the Ingestor URL as the endpoint to Collector,
			// which Collector will use to Proxy the request to Ingestor.
			collectorURL, hc := NewCollectorHandler(t, ctx, ingestorDir, []string{ingestorURL})

			// Now here we act on behalf of a logs producer, sending a valid OTLP logs payload to Collector.
			// We expect a valid response object and status code, after verification, we'll then test
			// the contents written to disk by Ingestor's store.
			u, err := url.JoinPath(collectorURL, tt.URLPath)
			require.NoError(t, err)

			var log v1.ExportLogsServiceRequest
			err = protojson.Unmarshal(rawlog, &log)
			require.NoError(t, err)

			buf, err := proto.Marshal(&log)
			require.NoError(t, err)

			resp, err := hc.Post(u, "application/x-protobuf", bytes.NewReader(buf))
			require.NoError(t, err)
			VerifyResponse(t, resp)

			// Wait for segments to flush
			time.Sleep(100 * time.Millisecond)

			cancel()
			<-done // Wait for the store to finish flushing

			// Now verify the content of our segments.
			VerifyStore(t, ingestorDir)

			// Ensure our internal Prometheus metrics state is as expected.
			VerifyMetrics(t, &log)
		})
	}
}

func VerifyResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	require.Equal(t, resp.StatusCode, http.StatusOK)

	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var log v1.ExportLogsServiceResponse
	err = proto.Unmarshal(b, &log)
	require.NoError(t, err)

	if p := log.GetPartialSuccess(); p != nil {
		require.Equal(t, 0, p.GetRejectedLogRecords())
		require.Equal(t, "", p.GetErrorMessage())
	}
}

func VerifyStore(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Equal(t, 2, len(entries))

	var verified bool
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".wal") {
			continue
		}

		info, err := entry.Info()
		require.NoError(t, err)
		require.NotEqual(t, 0, info.Size())

		// Ensure kusto metadata is present in the filename
		ss := strings.Split(entry.Name(), "_")
		require.Equal(t, 3, len(ss))
		require.Contains(t, ss[0], "Database")
		require.Contains(t, ss[1], "Table")

		s, err := wal.NewSegmentReader(filepath.Join(dir, entry.Name()))
		require.NoError(t, err)
		b, err := io.ReadAll(s)
		require.NoError(t, err)

		r := csv.NewReader(bytes.NewReader(b))
		records, err := r.ReadAll()
		require.NoError(t, err)
		require.NotEqual(t, 0, len(records))
		for _, record := range records {
			require.Equal(t, 9, len(record))
		}
		verified = true

		st, v := s.Metadata()
		require.Equal(t, "log", st.String())
		require.Equal(t, uint16(len(records)), v)
	}
	require.Equal(t, true, verified)
}

func NewCollectorHandler(t *testing.T, ctx context.Context, dir string, endpoints []string) (string, *http.Client) {
	t.Helper()
	var (
		insecureSkipVerify = true
		addAttributes      = map[string]string{
			"some-key":       "some-value",
			"some-other-key": "some-other-value",
		}
		liftAttributes = []string{"kusto.table", "kusto.database"}
	)
	mux := http.NewServeMux()
	logsProxySvc := cotlp.NewLogsProxyService(cotlp.LogsProxyServiceOpts{
		Endpoints:          endpoints,
		InsecureSkipVerify: insecureSkipVerify,
		AddAttributes:      addAttributes,
		LiftAttributes:     liftAttributes,
	})

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir: dir,
	})
	require.NoError(t, store.Open(ctx))

	logsTransferSvc := cotlp.NewLogsService(cotlp.LogsServiceOpts{
		Store:         store,
		AddAttributes: addAttributes,
	})
	mux.HandleFunc(logsv1connect.LogsServiceExportProcedure, logsProxySvc.Handler)
	mux.HandleFunc("/v1/logs", logsTransferSvc.Handler)

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	go func(srv *httptest.Server, t *testing.T) {
		t.Helper()
		<-ctx.Done()
		store.Close()
		srv.Close()
	}(srv, t)

	return srv.URL, srv.Client()
}

func NewIngestorHandler(t *testing.T, ctx context.Context, dir string) (string, <-chan struct{}) {
	t.Helper()

	store := storage.NewLocalStore(storage.StoreOpts{
		StorageDir: dir,
	})
	err := store.Open(ctx)
	require.NoError(t, err)

	writer := func(ctx context.Context, database, table string, logs *otlp.Logs) error {
		require.NotEmpty(t, database)
		require.NotEmpty(t, table)
		return store.WriteOTLPLogs(ctx, database, table, logs)
	}

	svc, err := isvc.NewService(isvc.ServiceOpts{
		StorageDir:      dir,
		Uploader:        adx.NewFakeUploader("ADatabase"),
		LogsDatabases:   []string{"ADatabase", "BDatabase"},
		MaxSegmentCount: 100,
		MaxDiskUsage:    10 * 1024 * 1024 * 1024,
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(logsv1connect.NewLogsServiceHandler(iotlp.NewLogsServiceHandler(writer, []string{"ADatabase", "BDatabase"})))
	mux.HandleFunc("/transfer", svc.HandleTransfer)

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	done := make(chan struct{})
	go func(srv *httptest.Server, t *testing.T) {
		t.Helper()
		<-ctx.Done()
		srv.Close()
		err := store.Close()
		require.NoError(t, err)
		done <- struct{}{}
	}(srv, t)

	return srv.URL, done
}

func VerifyMetrics(t *testing.T, log *v1.ExportLogsServiceRequest) {
	t.Helper()

	// First let's determine our expected state
	var (
		databaseA, databaseB int
		tableA, tableB       int
	)
	for _, r := range log.GetResourceLogs() {
		for _, s := range r.GetScopeLogs() {
			for _, l := range s.GetLogRecords() {
				d, t := otlp.KustoMetadata(l)
				switch d {
				case "ADatabase":
					databaseA++
					switch t {
					case "ATable":
						tableA++
					case "BTable":
						tableB++
					}
				case "BDatabase":
					databaseB++
					switch t {
					case "ATable":
						tableA++
					case "BTable":
						tableB++
					}
				}
			}
		}
	}

	// Now let's gather our metrics
	mets, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, m := range mets {
		switch m.GetName() {
		// Verify the Collector has the correct metrics for number of logs uploaded to Ingestor
		case "adxmon_collector_logs_uploaded_total":
			mm := m.GetMetric()
			require.Equal(t, 2, len(mm))

			for i := 0; i < len(mm); i++ {
				met := mm[i]
				labels := met.GetLabel()

				// Now let's verify our metric values
				switch labels[1].GetValue() {
				case "ATable":
					require.Equal(t, met.Counter.GetValue(), float64(tableA))
				case "BTable":
					require.Equal(t, met.Counter.GetValue(), float64(tableB))
				}
			}
			// As for Ingestor's metrics, we're stopping short at writing the
			// logs to storage, and VerifyStore already verifies the content
			// therein. If we ever expand this test to include a fake uploader,
			// that would be an opportunity to verify adxmon_ingestor_logs_uploaded_total
		}
	}
}
