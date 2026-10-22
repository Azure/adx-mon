package ingestor

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
	fakek8s "k8s.io/client-go/kubernetes/fake"
)

type fakeHealthChecker struct {
	healthy bool
}

func (f *fakeHealthChecker) IsHealthy() bool {
	return f.healthy
}

func TestService_HandleTransfer_DroppedPrefix(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store:  &fakeStore{},
		dropFilePrefixes: []string{
			"testdb_foo",
			"testdb_bar",
		},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	body := bytes.NewReader([]byte{0xde, 0xad, 0xbe, 0xef})
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)

	q := req.URL.Query()
	q.Add("filename", "testdb_bar_baz.wal")
	req.URL.RawQuery = q.Encode()

	// Silently dropped
	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
}

func TestService_HandleTransfer_MissingFilename(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		dropFilePrefixes: []string{
			"testdb_willnotgetthistable",
			"testdb_willnotgetthisothertable",
		},
	}

	body := bytes.NewReader([]byte{})
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestService_HandleTransfer_InvalidFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{name: "missing extension", filename: "foo"},
		{name: "invalid extension", filename: "foo.bar"},
		{name: "invalid separators", filename: "foo.bar.wal"},
		{name: "too many separators", filename: "foo_bar_baz_bip.wal"},
		{name: "not enough separators", filename: "foo_bar.wal"},
		{name: "no filename", filename: ""},
		{name: "path traversal", filename: "../../foo_bar_baz.wal"},
		{name: "colon", filename: "DB_Metric:avg_123.wal"},
		{name: "period", filename: "DB.wal_Metricavg_123.wal"},
		{name: "unknown DB", filename: "Database_Metric_123.wal"},
	}

	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store:  &fakeStore{},
		dropFilePrefixes: []string{
			"testdb_willnotgetthistable",
			"testdb_willnotgetthisothertable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewReader([]byte{})
			req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)

			q := req.URL.Query()
			q.Add("filename", tt.filename)
			req.URL.RawQuery = q.Encode()
			require.NoError(t, err)

			resp := httptest.NewRecorder()
			s.HandleTransfer(resp, req)
			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
		})
	}
}

func TestService_HandleTransfer_ValidFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{name: "valid file", filename: "testdb_testtable_testschema_1234567890.wal"},
	}

	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store:  &fakeStore{},
		dropFilePrefixes: []string{
			"testdb_willnotgetthistable",
			"testdb_willnotgetthisothertable",
		},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewReader([]byte{})
			req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)

			q := req.URL.Query()
			q.Add("filename", tt.filename)
			req.URL.RawQuery = q.Encode()
			require.NoError(t, err)

			resp := httptest.NewRecorder()
			s.HandleTransfer(resp, req)
			require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())
		})
	}
}

func TestService_HandleTransfer_BlockChecksumFailed(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store: &fakeStore{
			importFn: func(filename string, body io.ReadCloser) (int, error) {
				return 0, fmt.Errorf("block checksum verification failed")
			},
		},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	body := bytes.NewReader([]byte{})
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)

	q := req.URL.Query()
	q.Add("filename", "testdb_testtable_testschema_1234567890.wal")
	req.URL.RawQuery = q.Encode()
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestService_HandleTransfer_GzipEncodedBody(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store: &fakeStore{
			importFn: func(filename string, body io.ReadCloser) (int, error) {
				defer body.Close()
				data, err := io.ReadAll(body)
				require.Equal(t, "test data", string(data))
				if err != nil {
					return 0, err
				}
				if len(data) == 0 {
					return 0, fmt.Errorf("empty data")
				}
				return len(data), nil
			},
		},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	data := []byte("test data")
	var compressedData bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressedData)
	_, err := gzipWriter.Write(data)
	require.NoError(t, err)
	require.NoError(t, gzipWriter.Close())

	body := bytes.NewReader(compressedData.Bytes())
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")

	q := req.URL.Query()
	q.Add("filename", "testdb_testtable_testschema_1234567890.wal")
	req.URL.RawQuery = q.Encode()

	resp := httptest.NewRecorder()

	s.HandleTransfer(resp, req)

	require.Equal(t, http.StatusAccepted, resp.Code)

}

func TestService_HandleTransfer_InvalidGzipEncoding(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store:  &fakeStore{},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	body := bytes.NewReader([]byte("invalid gzip data"))
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")

	q := req.URL.Query()
	q.Add("filename", "testdb_testtable_testschema_1234567890.wal")
	req.URL.RawQuery = q.Encode()

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "Invalid gzip encoding")
}

func TestService_HandleTransfer_NoGzipHeader(t *testing.T) {
	s := &Service{
		health: &fakeHealthChecker{healthy: true},
		store: &fakeStore{
			importFn: func(filename string, body io.ReadCloser) (int, error) {
				defer body.Close()
				data, err := io.ReadAll(body)
				require.Equal(t, "test data", string(data))
				if err != nil {
					return 0, err
				}
				if len(data) == 0 {
					return 0, fmt.Errorf("empty data")
				}
				return len(data), nil
			},
		},
	}
	s.databases = make(map[string]struct{})
	s.databases["testdb"] = struct{}{}

	body := bytes.NewReader([]byte("test data"))
	req, err := http.NewRequest("POST", "http://localhost:8080/transfer", body)
	require.NoError(t, err)

	q := req.URL.Query()
	q.Add("filename", "testdb_testtable_testschema_1234567890.wal")
	req.URL.RawQuery = q.Encode()

	resp := httptest.NewRecorder()
	s.HandleTransfer(resp, req)
	require.Equal(t, http.StatusAccepted, resp.Code)
}

type fakeStore struct {
	segements map[string]struct{}
	importFn  func(filename string, body io.ReadCloser) (int, error)
}

func (f fakeStore) SegmentExists(filename string) bool {
	_, ok := f.segements[filename]
	return ok
}

func (f fakeStore) Open(ctx context.Context) error {
	panic("implement me")
}

func (f fakeStore) Close() error {
	panic("implement me")
}

func (f fakeStore) WriteTimeSeries(ctx context.Context, req *prompb.WriteRequest) error {
	panic("implement me")
}

func (f fakeStore) WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error {
	panic("implement me")
}

func (f fakeStore) WriteNativeLogs(ctx context.Context, logs *types.LogBatch) error {
	panic("implement me")
}

func (f fakeStore) IsActiveSegment(path string) bool {
	panic("implement me")
}

func (f fakeStore) Import(filename string, body io.ReadCloser) (int, error) {
	if f.importFn != nil {
		return f.importFn(filename, body)
	}
	return 0, nil
}

type capturingUploader struct {
	queue, realtime chan *cluster.Batch
}

func (u *capturingUploader) Open(context.Context) error               { return nil }
func (u *capturingUploader) Close() error                             { return nil }
func (u *capturingUploader) Database() string                         { return "" }
func (u *capturingUploader) UploadQueue() chan *cluster.Batch         { return u.queue }
func (u *capturingUploader) RealtimeUploadQueue() chan *cluster.Batch { return u.realtime }

// newTransferBody returns the bytes of a segment containing data.
func newTransferBody(t *testing.T, prefix string, data []byte) (string, []byte) {
	t.Helper()
	seg, err := wal.NewSegment(t.TempDir(), prefix)
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), data)
	require.NoError(t, err)
	require.NoError(t, seg.Close())
	b, err := os.ReadFile(seg.Path())
	require.NoError(t, err)
	return filepath.Base(seg.Path()), b
}

func TestService_RealtimeTransferIsBatchedPromptly(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Metrics", Table: "CpuUsage"}})
	require.NoError(t, err)

	uploader := &capturingUploader{queue: make(chan *cluster.Batch, 10), realtime: make(chan *cluster.Batch, 10)}
	var peers []cluster.PeerInfo
	svc, err := NewService(ServiceOpts{
		StorageDir:       t.TempDir(),
		Uploader:         uploader,
		MaxSegmentSize:   1024 * 1024,
		MaxSegmentAge:    time.Hour,
		MaxTransferAge:   time.Hour,
		MaxTransferSize:  1024 * 1024,
		MaxSegmentCount:  1000,
		MaxDiskUsage:     1024 * 1024 * 1024,
		K8sCli:           fakek8s.NewSimpleClientset(),
		Hostname:         "ingestor-0",
		MetricsDatabases: []string{"Metrics"},
		AllowedDatabase:  []string{"Metrics"},
		Realtime: &RealtimeOpts{
			Policy:            policy,
			MaxSegmentAge:     50 * time.Millisecond,
			MaxBatchLatency:   50 * time.Millisecond,
			MaxBatchBytes:     1024 * 1024,
			ReservedDiskBytes: 1024 * 1024,
		},
		PeerListeners: []func(cluster.PeerInfo){func(p cluster.PeerInfo) { peers = append(peers, p) }},
	})
	require.NoError(t, err)
	require.NoError(t, svc.Open(context.Background()))
	defer svc.Close()

	// Peer listeners receive the initial peer info when the service opens.
	require.Equal(t, []cluster.PeerInfo{{Count: 1, Rank: 0}}, peers)

	transfer := func(prefix string) {
		filename, body := newTransferBody(t, prefix, []byte("2024-01-01T00:00:00Z,1,{},1.5\n"))
		req, err := http.NewRequest("POST", "http://localhost:9090/transfer?filename="+filename, bytes.NewReader(body))
		require.NoError(t, err)
		resp := httptest.NewRecorder()
		svc.HandleTransfer(resp, req)
		require.Equal(t, http.StatusAccepted, resp.Code, resp.Body.String())
	}

	start := time.Now()
	transfer("Metrics_CpuUsage")
	transfer("Metrics_MemoryUsage")

	// The realtime segment rotates and is batched within its realtime latency, well before the queued segment's
	// one hour max age or the periodic scan.
	select {
	case batch := <-uploader.realtime:
		require.Equal(t, ingestpolicy.PriorityRealtime, batch.Priority)
		require.Equal(t, "CpuUsage", batch.Table)
		require.Less(t, time.Since(start), 2*time.Second)
	case <-time.After(5 * time.Second):
		t.Fatal("realtime batch was not uploaded")
	}
	require.Empty(t, uploader.queue)
}
