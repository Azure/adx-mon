package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	csvWriterPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return transform.NewCSVWriter(bytes.NewBuffer(make([]byte, 0, sz)), nil)
	})
	bytesPool = pool.NewBytes(1000)

	bytesBufPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})
)

type Store interface {
	service.Component

	// WriteTimeSeries writes a batch of time series to the Store.
	WriteTimeSeries(ctx context.Context, database string, ts []prompb.TimeSeries) error

	// WriteOTLPLogs writes a batch of logs to the Store.
	WriteOTLPLogs(ctx context.Context, database, table string, logs *otlp.Logs) error

	// IsActiveSegment returns true if the given path is an active segment.
	IsActiveSegment(path string) bool

	// Import imports a file into the LocalStore and returns the number of bytes stored.
	Import(filename string, body io.ReadCloser) (int, error)

	// SegmentExsts returns true if the given segment exists.
	SegmentExists(filename string) bool
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

	LiftedColumns []string
}

func NewLocalStore(opts StoreOpts) *LocalStore {
	return &LocalStore{
		opts: opts,
		repository: wal.NewRepository(wal.RepositoryOpts{
			StorageDir:     opts.StorageDir,
			SegmentMaxSize: opts.SegmentMaxSize,
			SegmentMaxAge:  opts.SegmentMaxAge,
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

func (s *LocalStore) WriteTimeSeries(ctx context.Context, database string, ts []prompb.TimeSeries) error {
	enc := csvWriterPool.Get(8 * 1024).(*transform.CSVWriter)
	defer csvWriterPool.Put(enc)
	enc.InitColumns(s.opts.LiftedColumns)

	b := bytesPool.Get(256)
	defer bytesPool.Put(b)

	db := []byte(database)
	for _, v := range ts {

		key := SegmentKey(b[:0], db, v.Labels)
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
	enc := csvWriterPool.Get(8 * 1024).(*transform.CSVWriter)
	defer csvWriterPool.Put(enc)

	key := bytesPool.Get(256)
	defer bytesPool.Put(key)

	if logger.IsDebug() {
		logger.Debugf("Store received %d logs for %s.%s", len(logs.Logs), database, table)
	}

	key = fmt.Appendf(key[:0], "%s_%s", database, table)

	wal, err := s.GetWAL(ctx, key)
	if err != nil {
		return err
	}

	metrics.SamplesStored.WithLabelValues(table).Add(float64(len(logs.Logs)))

	enc.Reset()
	if err := enc.MarshalCSV(logs); err != nil {
		return err
	}

	if err := wal.Write(ctx, enc.Bytes()); err != nil {
		return err
	}

	return nil
}

func (s *LocalStore) IsActiveSegment(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.repository.IsActiveSegment(path)
}

func (s *LocalStore) SegmentExists(filename string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.repository.SegmentExists(filename)
}

func (s *LocalStore) Import(filename string, body io.ReadCloser) (int, error) {
	db, table, _, err := wal.ParseFilename(filename)
	if err != nil {
		return 0, err
	}

	key := bytesPool.Get(256)
	defer bytesPool.Put(key)

	key = fmt.Appendf(key[:0], "%s_%s", db, table)

	wal, err := s.GetWAL(context.Background(), key)
	if err != nil {
		return 0, err
	}

	buf := bytesBufPool.Get(8 * 1024).(*bytes.Buffer)
	defer bytesBufPool.Put(buf)
	buf.Reset()

	n, err := io.Copy(buf, body)
	if err != nil {
		return 0, err
	}

	return int(n), wal.Append(context.Background(), buf.Bytes())
}

func (s *LocalStore) Remove(path string) error {
	db, table, _, err := wal.ParseFilename(path)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("%s_%s", db, table)
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

func SegmentKey(dst []byte, database []byte, labels []prompb.Label) []byte {
	dst = append(dst, database...)
	dst = append(dst, delim...)
	for _, v := range labels {
		if bytes.Equal(v.Name, []byte("__name__")) {
			// return fmt.Sprintf("%s%d", string(transform.Normalize(v.Value)), int(atomic.AddUint64(&idx, 1))%2)
			return transform.AppendNormalize(dst, v.Value)
		}
	}
	return transform.AppendNormalize(dst, labels[0].Value)
}

var delim = []byte("_")
