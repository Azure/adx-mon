package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/schema"
	transform2 "github.com/Azure/adx-mon/transform"
	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	csvWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform2.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	nativeLogsCSVWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform2.NewCSVNativeLogsCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	metricsCSVWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform2.NewMetricsCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})

	bytesBufPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})
)

type Store interface {
	service.Component

	// WriteTimeSeries writes a batch of time series to the Store.
	WriteTimeSeries(ctx context.Context, ts []*prompb.TimeSeries) error

	// WriteOTLPLogs writes a batch of logs to the Store.
	WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error

	// WriteNativeLogs writes a batch of logs to the Store.
	WriteNativeLogs(ctx context.Context, logs *types.LogBatch) error

	// Import imports a file into the LocalStore and returns the number of bytes stored.
	Import(filename string, body io.ReadCloser) (int, error)
}

// LocalStore provides local storage of time series data.  It manages Write Ahead Logs (WALs) for each metric.
type LocalStore struct {
	opts StoreOpts

	mu         sync.RWMutex
	repository *wal.Repository

	metricsMu sync.RWMutex
	metrics   map[string]prometheus.Counter
}

type StoreOpts struct {
	StorageDir     string
	SegmentMaxSize int64
	SegmentMaxAge  time.Duration
	MaxDiskUsage   int64

	LiftedLabels     []string
	LiftedAttributes []string
	LiftedResources  []string
	WALFlushInterval time.Duration
}

func NewLocalStore(opts StoreOpts) *LocalStore {
	return &LocalStore{
		opts: opts,
		repository: wal.NewRepository(wal.RepositoryOpts{
			StorageDir:       opts.StorageDir,
			SegmentMaxSize:   opts.SegmentMaxSize,
			SegmentMaxAge:    opts.SegmentMaxAge,
			MaxDiskUsage:     opts.MaxDiskUsage,
			WALFlushInterval: opts.WALFlushInterval,
		}),
		metrics: make(map[string]prometheus.Counter),
	}
}

func (s *LocalStore) Open(ctx context.Context) error {
	return s.repository.Open(ctx)
}

func (s *LocalStore) Close() error {
	return s.repository.Close()
}

func (s *LocalStore) GetWAL(ctx context.Context, key []byte) (*wal.WAL, error) {
	return s.repository.Get(ctx, key)
}

func (s *LocalStore) WALCount() int {
	return s.repository.Count()
}

func (s *LocalStore) WriteTimeSeries(ctx context.Context, ts []*prompb.TimeSeries) error {
	enc := metricsCSVWriterPool.Get(8 * 1024).(*transform2.MetricsCSVWriter)
	defer metricsCSVWriterPool.Put(enc)
	enc.InitColumns(s.opts.LiftedLabels)

	b := gbp.Get(256)
	defer gbp.Put(b)

	for _, v := range ts {
		key, err := SegmentKey(b[:0], v.Labels, enc.SchemaHash())
		if err != nil {
			return err
		}

		wal, err := s.GetWAL(ctx, key)
		if err != nil {
			return err
		}

		s.incMetrics(v.Labels[0].Value, len(v.Samples))

		enc.Reset()
		if err := enc.MarshalCSV(v); err != nil {
			return err
		}

		if err := wal.Write(ctx, enc.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStore) WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error {
	sanitizedDB := schema.NormalizeAdxIdentifier(database)
	sanitizedTable := schema.NormalizeAdxIdentifier(table)

	if sanitizedDB == "" || sanitizedTable == "" {
		logger.Warnf("Invalid database or table name: %s.%s", database, table)
		return nil // Do not retry - move on
	}

	enc := csvWriterPool.Get(8 * 1024).(*transform2.CSVWriter)
	defer csvWriterPool.Put(enc)

	key := gbp.Get(256)
	defer gbp.Put(key)

	if logger.IsDebug() {
		logger.Debugf("Store received %d logs for %s.%s", len(logs.Logs), sanitizedDB, sanitizedTable)
		for _, log := range logs.Logs {
			if l, err := protojson.Marshal(log); err == nil {
				logger.Debugf("Log: %s", l)
			}
		}
	}

	key = fmt.Appendf(key[:0], "%s_%s", sanitizedDB, sanitizedTable)

	w, err := s.GetWAL(ctx, key)
	if err != nil {
		return err
	}

	metrics.SamplesStored.WithLabelValues(sanitizedTable).Add(float64(len(logs.Logs)))

	enc.Reset()
	if err := enc.MarshalLog(logs); err != nil {
		return err
	}

	if logger.IsDebug() {
		logger.Debugf("Marshaled logs: %s", enc.Bytes())
	}

	wo := wal.WithSampleMetadata(wal.LogSampleType, uint32(len(logs.Logs)))
	if err := w.Write(ctx, enc.Bytes(), wo); err != nil {
		return err
	}

	return nil
}

func (s *LocalStore) WriteNativeLogs(ctx context.Context, logs *types.LogBatch) error {
	enc := nativeLogsCSVWriterPool.Get(8 * 1024).(*transform2.NativeLogsCSVWriter)
	defer nativeLogsCSVWriterPool.Put(enc)
	enc.InitColumns(s.opts.LiftedResources)

	key := gbp.Get(256)
	defer gbp.Put(key)

	if logger.IsDebug() {
		logger.Debugf("Store received %d native logs", len(logs.Logs))
	}

	noDestinationCount := 0

	// Each log can potentially have a different destination.
	// Instead of splitting ahead of time and allocating n slices, just encode and write to the wal on
	// a per-log basis.
	for _, log := range logs.Logs {
		// If we don't have a destination, we can't do anything with the log.
		// Skip instead of trying again, which will just repeat the same error.
		database, ok := log.Attributes[types.AttributeDatabaseName].(string)
		if !ok || database == "" {
			noDestinationCount++
			continue
		}
		sanitizedDB := schema.NormalizeAdxIdentifier(database)
		if sanitizedDB == "" {
			noDestinationCount++
			continue
		}

		table, ok := log.Attributes[types.AttributeTableName].(string)
		if !ok || table == "" {
			noDestinationCount++
			continue
		}
		sanitizedTable := schema.NormalizeAdxIdentifier(table)
		if sanitizedTable == "" {
			noDestinationCount++
			continue
		}

		key = fmt.Appendf(key[:0], "%s_%s_", sanitizedDB, sanitizedTable)
		key = strconv.AppendUint(key, enc.SchemaHash(), 36)

		wal, err := s.GetWAL(ctx, key)
		if err != nil {
			return err
		}

		metrics.SamplesStored.WithLabelValues(table).Inc()

		enc.Reset()
		if err := enc.MarshalNativeLog(log); err != nil {
			return err
		}

		if err := wal.Write(ctx, enc.Bytes()); err != nil {
			return err
		}
	}

	if noDestinationCount > 0 {
		logger.Warnf("Got %d logs without ADX destinations - dropped", noDestinationCount)
		metrics.InvalidLogsDropped.WithLabelValues("no_destination").Add(float64(noDestinationCount))
	}

	return nil
}

func (s *LocalStore) PrefixesByAge() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repository.PrefixesByAge()
}

func (s *LocalStore) Import(filename string, body io.ReadCloser) (int, error) {
	db, table, schema, _, err := wal.ParseFilename(filename)
	if err != nil {
		return 0, err
	}

	key := gbp.Get(256)
	defer gbp.Put(key)

	if schema != "" {
		key = fmt.Appendf(key[:0], "%s_%s_%s", db, table, schema)
	} else {
		key = fmt.Appendf(key[:0], "%s_%s", db, table)
	}

	wal, err := s.GetWAL(context.Background(), key)
	if err != nil {
		return 0, err
	}

	buf := pool.BytesBufferPool.Get(512 * 1024).(*gbp.Buffer)
	defer pool.BytesBufferPool.Put(buf)
	buf.Reset()

	n, err := io.Copy(buf, body)
	if err != nil {
		return 0, err
	}

	return int(n), wal.Append(context.Background(), buf.Bytes())
}

func (s *LocalStore) Remove(path string) error {
	db, table, schema, _, err := wal.ParseFilename(path)
	if err != nil {
		return err
	}

	var key string
	if schema != "" {
		key = fmt.Sprintf("%s_%s_%s", db, table, schema)
	} else {
		key = fmt.Sprintf("%s_%s", db, table)
	}

	wal, err := s.GetWAL(context.Background(), []byte(key))
	if err != nil {
		return err
	}
	return wal.Remove(path)
}

func (s *LocalStore) incMetrics(value []byte, n int) {
	s.metricsMu.RLock()
	counter := s.metrics[string(value)]
	s.metricsMu.RUnlock()

	if counter != nil {
		counter.Add(float64(n))
		return
	}

	s.metricsMu.Lock()
	counter = s.metrics[string(value)]
	if counter == nil {
		counter = metrics.SamplesStored.WithLabelValues(string(value))
		s.metrics[string(value)] = counter
	}
	s.metricsMu.Unlock()

	counter.Add(float64(n))
}

func (s *LocalStore) Index() *wal.Index {
	return s.repository.Index()
}

func SegmentKey(dst []byte, labels []*prompb.Label, hash uint64) ([]byte, error) {
	var name, database []byte
	for _, v := range labels {
		if bytes.Equal(v.Name, []byte("adxmon_database")) {
			database = v.Value
			continue
		}

		if bytes.Equal(v.Name, []byte("__name__")) {
			name = v.Value
			continue
		}
	}

	if len(database) == 0 {
		return nil, fmt.Errorf("database label not found")
	}

	if len(name) == 0 {
		return nil, fmt.Errorf("name label not found")
	}

	dst = schema.AppendNormalizeAdxIdentifier(dst, database)
	dst = append(dst, delim...)
	dst = schema.AppendNormalizeMetricName(dst, name)
	dst = append(dst, delim...)
	return strconv.AppendUint(dst, hash, 36), nil
}

var delim = []byte("_")
