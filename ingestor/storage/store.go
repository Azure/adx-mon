package storage

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/service"
)

type Store interface {
	service.Component

	// WriteTimeSeries writes a batch of time series to the Store.
	WriteTimeSeries(ctx context.Context, ts []prompb.TimeSeries) error

	// IsActiveSegment returns true if the given path is an active segment.
	IsActiveSegment(path string) bool

	// Import imports a file into the LocalStore and returns the number of bytes stored.
	Import(filename string, body io.ReadCloser) (int, error)
}

// LocalStore provides local storage of time series data.  It manages Write Ahead Logs (WALs) for each metric.
type LocalStore struct {
	opts StoreOpts

	mu   sync.RWMutex
	wals map[string]*WAL
}

type StoreOpts struct {
	StorageDir     string
	SegmentMaxSize int64
	SegmentMaxAge  time.Duration
}

func NewLocalStore(opts StoreOpts) *LocalStore {
	return &LocalStore{
		opts: opts,
		wals: make(map[string]*WAL),
	}
}

func (s *LocalStore) Open(ctx context.Context) error {
	return filepath.WalkDir(s.opts.StorageDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".csv" {
			return nil
		}

		fileName := filepath.Base(path)
		fields := strings.Split(fileName, "_")
		if len(fields) != 2 {
			logger.Warn("Invalid WAL segment: %s", path)
			return nil
		}

		prefix := fields[0]
		_, ok := s.wals[prefix]
		if ok {
			return nil
		}

		wal, err := s.newWAL(ctx, prefix)
		if err != nil {
			return err
		}
		s.wals[prefix] = wal

		return nil
	})

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

func (s *LocalStore) newWAL(ctx context.Context, prefix string) (*WAL, error) {
	walOpts := WALOpts{
		StorageDir:     s.opts.StorageDir,
		Prefix:         prefix,
		SegmentMaxSize: s.opts.SegmentMaxSize,
		SegmentMaxAge:  s.opts.SegmentMaxAge,
	}

	wal, err := NewWAL(walOpts)
	if err != nil {
		return nil, err
	}

	if err := wal.Open(ctx); err != nil {
		return nil, err
	}

	return wal, nil
}

func (s *LocalStore) GetWAL(ctx context.Context, labels []prompb.Label) (*WAL, error) {
	key := seriesKey(labels)

	s.mu.RLock()
	wal := s.wals[key]
	s.mu.RUnlock()

	if wal != nil {
		return wal, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	wal = s.wals[key]
	if wal != nil {
		return wal, nil
	}

	prefix := key

	var err error
	wal, err = s.newWAL(ctx, prefix)
	if err != nil {
		return nil, err
	}
	s.wals[key] = wal

	return wal, nil
}

func (s *LocalStore) WALCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wals)
}

func (s *LocalStore) WriteTimeSeries(ctx context.Context, ts []prompb.TimeSeries) error {
	for _, v := range ts {
		wal, err := s.GetWAL(ctx, v.Labels)
		if err != nil {
			return err
		}

		if err := wal.Write(ctx, []prompb.TimeSeries{v}); err != nil {
			return err
		}
	}
	return nil
}

func (s *LocalStore) IsActiveSegment(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, v := range s.wals {
		if v.SegmentPath() == path {
			return true
		}
	}
	return false
}

func (s *LocalStore) Import(filename string, body io.ReadCloser) (int, error) {
	dstPath := filepath.Join(s.opts.StorageDir, fmt.Sprint(filename, ".tmp"))
	f, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}

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

	if err := os.Rename(dstPath, filepath.Join(s.opts.StorageDir, filename)); err != nil {
		return 0, err
	}

	return int(n), nil
}

var idx uint64

func seriesKey(labels []prompb.Label) string {
	for _, v := range labels {
		if bytes.Equal(v.Name, []byte("__name__")) {
			// return fmt.Sprintf("%s%d", string(transform.Normalize(v.Value)), int(atomic.AddUint64(&idx, 1))%2)
			return string(transform.Normalize(v.Value))
		}
	}
	return string(transform.Normalize(labels[0].Value))
}
