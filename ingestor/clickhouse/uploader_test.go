package clickhouse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	"github.com/stretchr/testify/require"
)

func TestConfigValidateErrors(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	err := cfg.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "database is required")
	require.ErrorContains(t, err, "DSN is required")
}

func TestNewUploaderAppliesDefaults(t *testing.T) {
	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://localhost:9000/default",
	}

	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	impl, ok := u.(*uploader)
	require.True(t, ok)
	require.Equal(t, DefaultQueueCapacity, cap(impl.queue))
	require.Equal(t, DefaultBatchMaxRows, impl.cfg.Batch.MaxRows)
	require.Equal(t, DefaultBatchFlushInterval, impl.cfg.Batch.FlushInterval)
	require.Equal(t, cfg.Database, impl.Database())
	require.Equal(t, cfg.DSN, impl.DSN())
	require.NotNil(t, impl.log)
	require.NotNil(t, impl.schemas)
}

func TestNewUploaderValidatesTLSPairing(t *testing.T) {
	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://localhost:9000/default",
		TLS: TLSConfig{
			CertFile: "cert.pem",
		},
	}

	_, err := NewUploader(cfg, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "tls cert file")
}

func TestOpenCloseIsIdempotent(t *testing.T) {
	fake := &fakeConnection{}
	withFakeConnectionManager(t, fake)

	cfg := Config{
		Database:      "metrics",
		DSN:           "clickhouse://localhost:9000/default",
		QueueCapacity: 16,
	}

	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, u.Open(ctx))
	require.NoError(t, u.Open(ctx))
	require.NoError(t, u.Close())
	require.NoError(t, u.Close())
}

func TestUploaderOpenFailsWhenSchemaSyncFails(t *testing.T) {
	fake := &fakeConnection{execErr: errors.New("sync failed")}
	withFakeConnectionManager(t, fake)

	cfg := Config{
		Database: "metrics",
		DSN:      "clickhouse://localhost:9000/default",
	}

	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	err = u.Open(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "sync failed")
}

func TestSchemasReturnsCopy(t *testing.T) {
	withFakeConnectionManager(t, &fakeConnection{})

	cfg := Config{Database: "metrics", DSN: "clickhouse://localhost"}
	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	impl := u.(*uploader)
	first := impl.Schemas()
	first["metrics"] = Schema{Table: "hacked"}

	second := impl.Schemas()
	require.NotEqual(t, "hacked", second["metrics"].Table)
}

func TestUploaderProcessesMetricBatch(t *testing.T) {
	fake := &fakeConnection{}
	withFakeConnectionManager(t, fake)

	cfg := Config{Database: "metricsdb", DSN: "clickhouse://localhost"}
	u, err := NewUploader(cfg, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, u.Open(ctx))
	t.Cleanup(func() {
		require.NoError(t, u.Close())
	})

	storageDir := t.TempDir()
	prefix := "metricsdb_CpuUsage_schema"

	seg, err := wal.NewSegment(storageDir, prefix)
	require.NoError(t, err)

	timestamp := time.Now().UTC().Truncate(time.Second)
	header := schema.AppendCSVHeader(make([]byte, 0, 128), schema.DefaultMetricsMapping)

	var buf bytes.Buffer
	buf.Write(header)
	buf.WriteString(fmt.Sprintf("%s,0,\"{}\",42.5\n", timestamp.Format(time.RFC3339Nano)))
	buf.WriteString(strings.TrimSpace(string(header)))
	buf.WriteByte('\n')

	_, err = seg.Write(context.Background(), buf.Bytes())
	require.NoError(t, err)

	info := seg.Info()
	require.NoError(t, seg.Close())

	batch := &cluster.Batch{
		Segments: []wal.SegmentInfo{info},
		Database: "metricsdb",
		Table:    "CpuUsage",
		Prefix:   "metricsdb_CpuUsage",
	}
	batcher := &stubBatcher{}
	setBatcher(t, batch, batcher)

	u.UploadQueue() <- batch

	require.Eventually(t, func() bool {
		b := fake.lastBatch()
		if b == nil {
			return false
		}
		rows, sent, _ := b.snapshot()
		return sent && len(rows) == 1
	}, 2*time.Second, 10*time.Millisecond)

	b := fake.lastBatch()
	require.NotNil(t, b)
	rows, sent, closed := b.snapshot()
	require.True(t, sent)
	require.True(t, closed)
	require.Len(t, rows, 1)

	row := rows[0]
	require.Len(t, row, 4)
	require.Equal(t, timestamp, row[0])
	require.Equal(t, uint64(0), row[1])
	require.Equal(t, "{}", row[2])
	require.Equal(t, 42.5, row[3])

	require.Eventually(t, func() bool { return batch.IsRemoved() }, time.Second, 10*time.Millisecond)
	require.True(t, batch.IsReleased())
	require.True(t, batcher.WasRemoved())
	require.True(t, batcher.WasReleased())

	require.Eventually(t, func() bool {
		for _, query := range fake.execQueries() {
			if strings.Contains(query, "`metricsdb`.`CpuUsage`") {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		for _, query := range fake.execQueries() {
			if strings.Contains(query, "'CpuUsage'") {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)
}

type fakeConnection struct {
	pingErr    error
	prepareErr error
	execErr    error

	mu      sync.Mutex
	batches []*fakeBatch
	execs   []string
}

func (f *fakeConnection) Ping(context.Context) error { return f.pingErr }
func (f *fakeConnection) Close() error               { return nil }

func (f *fakeConnection) Exec(_ context.Context, query string, args ...any) error {
	if f.execErr != nil {
		return f.execErr
	}
	f.mu.Lock()
	query = strings.TrimSpace(query)
	if len(args) > 0 {
		query = fmt.Sprintf("%s | args=%d", query, len(args))
	}
	f.execs = append(f.execs, query)
	f.mu.Unlock()
	return nil
}

func (f *fakeConnection) PrepareInsert(context.Context, string, string, []Column) (batchWriter, error) {
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}

	batch := &fakeBatch{}
	f.mu.Lock()
	f.batches = append(f.batches, batch)
	f.mu.Unlock()
	return batch, nil
}

func (f *fakeConnection) lastBatch() *fakeBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.batches) == 0 {
		return nil
	}
	return f.batches[len(f.batches)-1]
}

func (f *fakeConnection) execCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.execs)
}

func (f *fakeConnection) execQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.execs))
	copy(out, f.execs)
	return out
}

type fakeBatch struct {
	mu      sync.Mutex
	rows    [][]any
	flushes int
	sent    bool
	closed  bool
}

func (b *fakeBatch) Append(values ...any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	row := make([]any, len(values))
	copy(row, values)
	b.rows = append(b.rows, row)
	return nil
}

func (b *fakeBatch) Flush() error {
	b.mu.Lock()
	b.flushes++
	b.mu.Unlock()
	return nil
}

func (b *fakeBatch) Send() error {
	b.mu.Lock()
	b.sent = true
	b.mu.Unlock()
	return nil
}

func (b *fakeBatch) Close() error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	return nil
}

func (b *fakeBatch) snapshot() ([][]any, bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rows := make([][]any, len(b.rows))
	for i, row := range b.rows {
		cloned := make([]any, len(row))
		copy(cloned, row)
		rows[i] = cloned
	}
	return rows, b.sent, b.closed
}

type stubBatcher struct {
	mu       sync.Mutex
	removed  bool
	released bool
}

func (s *stubBatcher) Open(context.Context) error { return nil }
func (s *stubBatcher) Close() error               { return nil }
func (s *stubBatcher) BatchSegments() error       { return nil }
func (s *stubBatcher) UploadQueueSize() int       { return 0 }
func (s *stubBatcher) TransferQueueSize() int     { return 0 }
func (s *stubBatcher) SegmentsTotal() int64       { return 0 }
func (s *stubBatcher) SegmentsSize() int64        { return 0 }
func (s *stubBatcher) MaxSegmentAge() time.Duration {
	return 0
}

func (s *stubBatcher) Release(*cluster.Batch) {
	s.mu.Lock()
	s.released = true
	s.mu.Unlock()
}

func (s *stubBatcher) Remove(*cluster.Batch) error {
	s.mu.Lock()
	s.removed = true
	s.mu.Unlock()
	return nil
}

func (s *stubBatcher) WasReleased() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.released
}

func (s *stubBatcher) WasRemoved() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.removed
}

func setBatcher(t *testing.T, batch *cluster.Batch, impl cluster.Batcher) {
	t.Helper()
	v := reflect.ValueOf(batch).Elem().FieldByName("batcher")
	if !v.IsValid() {
		t.Fatalf("batch does not contain batcher field")
	}
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(impl))
}

func withFakeConnectionManager(t *testing.T, conn connectionManager) {
	t.Helper()
	original := newConnectionManager
	newConnectionManager = func(Config) (connectionManager, error) {
		return conn, nil
	}
	t.Cleanup(func() {
		newConnectionManager = original
	})
}
