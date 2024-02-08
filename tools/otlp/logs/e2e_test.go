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
	"strconv"
	"strings"
	"testing"
	"time"

	"buf.build/gen/go/opentelemetry/opentelemetry/bufbuild/connect-go/opentelemetry/proto/collector/logs/v1/logsv1connect"
	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	cotlp "github.com/Azure/adx-mon/collector/otlp"
	isvc "github.com/Azure/adx-mon/ingestor"
	"github.com/Azure/adx-mon/ingestor/adx"
	"github.com/Azure/adx-mon/ingestor/cluster"
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
		{
			URLPath: logsv1connect.LogsServiceExportProcedure,
		},
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

			// TODO: temporarily disabling verification of metrics until
			// we enable sample writing.
			// VerifyMetrics(t, &log)
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
		// require.NotEqual(t, 0, len(records))
		for _, record := range records {
			require.Equal(t, 9, len(record))
		}
		verified = true

		// TODO: temporarily disabling verification of metrics until
		// we enable sample writing.
		if false {

			sampleType, sampleCount := s.SampleMetadata()
			require.Equal(t, wal.LogSampleType, sampleType)

			if ss[1] == "ATable" {
				require.Equal(t, uint16(2), sampleCount)
			} else {
				require.Equal(t, uint16(1), sampleCount)
			}
		}
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

func TestSampleMetadata(t *testing.T) {
	// The intent of this test is to ensure the path taken from storage
	// write on Collector all the way to Ingestor read to upload, correctly
	// conveys sample metadata.

	var (
		ctx            = context.Background()
		key            = []byte("Database_Table")
		ingestorDir    = t.TempDir()
		collectorDir   = t.TempDir()
		segmentCount   = 100
		samplesWritten = 0
		transferChan   = make(chan struct{})
	)

	// The following components are owned by the Ingestor's code paths
	ingestorStore := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     ingestorDir,
		SegmentMaxSize: 1024 * 1024,
		SegmentMaxAge:  time.Second,
	})
	require.NoError(t, ingestorStore.Open(ctx))

	mux := http.NewServeMux()
	mux.HandleFunc("/transfer", func(w http.ResponseWriter, r *http.Request) {
		filename := r.URL.Query().Get("filename")
		_, err := ingestorStore.Import(filename, r.Body)
		require.NoError(t, err)

		w.WriteHeader(http.StatusAccepted)
		require.NoError(t, r.Body.Close())

		// inform our main thread that the transfer has completed
		transferChan <- struct{}{}

	})

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	// The follow components are owned by the Collector's code paths
	collectorStore := storage.NewLocalStore(storage.StoreOpts{
		StorageDir:     collectorDir,
		SegmentMaxSize: 1024 * 1024,
		SegmentMaxAge:  time.Second,
	})
	require.NoError(t, collectorStore.Open(ctx))

	partitioner := remotePartitioner{
		host: "remote",
		addr: srv.URL,
	}

	replicator, err := cluster.NewReplicator(cluster.ReplicatorOpts{
		Hostname:           "fake",
		Partitioner:        partitioner,
		Health:             fakeHealthChecker{},
		SegmentRemover:     collectorStore,
		InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	batcher := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:         collectorDir,
		MaxSegmentAge:      time.Second,
		MinUploadSize:      1,
		Partitioner:        partitioner,
		Segmenter:          collectorStore.Index(),
		UploadQueue:        replicator.TransferQueue(),
		TransferQueue:      replicator.TransferQueue(),
		PeerHealthReporter: fakeHealthChecker{},
	})

	// We'll use the code path taken by logs_transfer, but keep the payload generic.
	// In our scenario, WriteOTLPLogs is the entrypoint, so we're going to use that
	// as our kicking off point.

	// We want to create several segments, since that's the most realistic usecase.
	for i := 0; i < segmentCount; i++ {
		w, err := collectorStore.GetWAL(ctx, key)
		require.NoError(t, err)

		wo := wal.WithSampleMetadata(wal.LogSampleType, uint32(i+1))
		require.NoError(t, w.Write(ctx, bytes.Repeat([]byte(strconv.Itoa(i)), 1024), wo))
		samplesWritten += i + 1

		// TODO: Write* in Store never actually invokes flush or close, should it?
		require.NoError(t, w.Flush())
		require.NoError(t, w.Close())
	}
	require.Equal(t, 1, collectorStore.WALCount())

	// We don't want to start batcher or replicator until all our bytes have been
	// flushed to disk so we have a known starting point.
	require.NoError(t, replicator.Open(ctx))
	require.NoError(t, batcher.Open(ctx))

	// Now we wait for Collector to transfer to Ingestor
	// Segments are sent by Batcher to Replicator one at a time
	require.NoError(t, batcher.BatchSegments())
	for i := 0; i < segmentCount; i++ {
		<-transferChan
	}
	// Have to wait for everything to flush...
	time.Sleep(time.Second)

	// Now shut down the Collector side, we don't want any interference with the test
	require.NoError(t, batcher.Close())
	require.NoError(t, replicator.Close())
	require.NoError(t, collectorStore.Close())

	// Collector is shut down, let's make sure its storage is empty since we
	// expect everything was transferred to Ingestor.
	entries, err := os.ReadDir(collectorDir)
	require.NoError(t, err)
	require.Equal(t, 0, len(entries))

	// From this point on we're inspecting Ingestor's code path from
	// the perspective of adx/uploader.
	var (
		readers        []io.Reader
		segmentReaders []*wal.SegmentReader
	)
	entries, err = os.ReadDir(ingestorDir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		f, err := wal.NewSegmentReader(filepath.Join(ingestorDir, entry.Name()))
		require.NoError(t, err)
		readers = append(readers, f)
		segmentReaders = append(segmentReaders, f)
	}

	mr := io.MultiReader(readers...)
	_, err = io.Copy(io.Discard, mr)
	require.NoError(t, err)

	var samplesRead int
	for _, sr := range segmentReaders {
		sampleType, sampleCount := sr.SampleMetadata()
		require.Equal(t, wal.LogSampleType, sampleType)
		samplesRead += int(sampleCount)
	}
	require.Equal(t, samplesWritten, samplesRead)
}

type fakeHealthChecker struct{}

func (f fakeHealthChecker) IsPeerHealthy(peer string) bool { return true }
func (f fakeHealthChecker) SetPeerUnhealthy(peer string)   {}
func (f fakeHealthChecker) SetPeerHealthy(peer string)     {}
func (f fakeHealthChecker) TransferQueueSize() int         { return 0 }
func (f fakeHealthChecker) UploadQueueSize() int           { return 0 }
func (f fakeHealthChecker) SegmentsTotal() int64           { return 0 }
func (f fakeHealthChecker) SegmentsSize() int64            { return 0 }
func (f fakeHealthChecker) IsHealthy() bool                { return true }

// remotePartitioner is a Partitioner that always returns the same owner that forces a remove transfer.
type remotePartitioner struct {
	host, addr string
}

func (f remotePartitioner) Owner(bytes []byte) (string, string) {
	return f.host, f.addr
}
