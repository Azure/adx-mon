package storage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	mu   sync.RWMutex
	wals map[string]*wal.WAL

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
		opts:    opts,
		wals:    make(map[string]*wal.WAL),
		metrics: make(map[string]prometheus.Counter),
	}
}

func (s *LocalStore) Open(ctx context.Context) error {
	wals, err := wal.ListDir(s.opts.StorageDir)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", s.opts.StorageDir, err)
	}
	for _, w := range wals {
		prefix := fmt.Sprintf("%s_%s", w.Database, w.Table)
		_, ok := s.wals[prefix]
		if ok {
			return nil
		}

		wal, err := s.newWAL(ctx, prefix)
		if err != nil {
			return err
		}
		s.wals[prefix] = wal
	}
	return nil
}

func (s *LocalStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range s.wals {
		if err := v.Close(); err != nil {
			return err
		}
	}
	s.wals = nil
	return nil
}

func (s *LocalStore) newWAL(ctx context.Context, prefix string) (*wal.WAL, error) {
	walOpts := wal.WALOpts{
		StorageDir:     s.opts.StorageDir,
		Prefix:         prefix,
		SegmentMaxSize: s.opts.SegmentMaxSize,
		SegmentMaxAge:  s.opts.SegmentMaxAge,
	}

	wal, err := wal.NewWAL(walOpts)
	if err != nil {
		return nil, err
	}

	if err := wal.Open(ctx); err != nil {
		return nil, err
	}

	return wal, nil
}

func (s *LocalStore) GetWAL(ctx context.Context, key []byte) (*wal.WAL, error) {

	s.mu.RLock()
	wal := s.wals[string(key)]
	s.mu.RUnlock()

	if wal != nil {
		return wal, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	wal = s.wals[string(key)]
	if wal != nil {
		return wal, nil
	}

	prefix := key

	var err error
	wal, err = s.newWAL(ctx, string(prefix))
	if err != nil {
		return nil, err
	}
	s.wals[string(key)] = wal

	return wal, nil
}

func (s *LocalStore) WALCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wals)
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

	for _, v := range s.wals {
		if v.Path() == path {
			return true
		}
	}
	return false
}

func (s *LocalStore) SegmentExists(filename string) bool {
	_, err := os.Stat(filepath.Join(s.opts.StorageDir, filename))
	return err == nil
}

func (s *LocalStore) Import(filename string, body io.ReadCloser) (int, error) {
	dstPath := filepath.Join(s.opts.StorageDir, fmt.Sprint(filename, ".tmp"))
	f, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}

	if n, err := func() (int, error) {
		bw := bufio.NewWriterSize(f, 16*1024)

		n, err := io.Copy(bw, body)
		if err != nil {
			return 0, err
		}

		if err := bw.Flush(); err != nil {
			return 0, err
		}

		if err := f.Sync(); err != nil {
			return 0, err
		}

		if err := f.Close(); err != nil {
			return 0, err
		}
		return int(n), nil
	}(); err != nil {
		_ = os.Remove(dstPath)
		return n, err
	} else {
		if err := os.Rename(dstPath, filepath.Join(s.opts.StorageDir, filename)); err != nil {
			return 0, err
		}
		return n, nil
	}
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
