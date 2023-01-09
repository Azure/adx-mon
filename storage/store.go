package storage

import (
	"bytes"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/transform"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	opts             StoreOpts
	closedSegmentsCh chan Segment

	mu   sync.RWMutex
	wals map[string]*WAL
}

type StoreOpts struct {
	StorageDir     string
	SegmentMaxSize int64
	SegmentMaxAge  time.Duration
}

func NewStore(opts StoreOpts) *Store {
	return &Store{
		opts:             opts,
		wals:             make(map[string]*WAL),
		closedSegmentsCh: make(chan Segment, 10*1024),
	}
}

func (s *Store) Open() error {
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
			logger.Warn("Invalid WAL file: %s", path)
			return nil
		}

		prefix := fields[0]
		_, ok := s.wals[prefix]
		if ok {
			return nil
		}

		wal, err := s.newWAL(prefix)
		if err != nil {
			return err
		}
		s.wals[prefix] = wal

		return nil
	})

}

func (s *Store) Close() error {
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

func (s *Store) newWAL(prefix string) (*WAL, error) {
	walOpts := WALOpts{
		ClosedSegmentCh: s.closedSegmentsCh,
		StorageDir:      s.opts.StorageDir,
		Prefix:          prefix,
		SegmentMaxSize:  s.opts.SegmentMaxSize,
		SegmentMaxAge:   s.opts.SegmentMaxAge,
	}

	wal, err := NewWAL(walOpts)
	if err != nil {
		return nil, err
	}

	if err := wal.Open(); err != nil {
		return nil, err
	}

	return wal, nil
}

func (s *Store) GetWAL(labels []prompb.Label) (*WAL, error) {
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
	wal, err = s.newWAL(prefix)
	if err != nil {
		return nil, err
	}
	s.wals[key] = wal

	return wal, nil
}

func (s *Store) WALCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wals)
}

func (s *Store) WriteTimeSeries(ts prompb.TimeSeries) error {
	wal, err := s.GetWAL(ts.Labels)
	if err != nil {
		return err
	}

	if err := wal.Write(ts); err != nil {
		return err
	}
	return nil
}

func seriesKey(labels []prompb.Label) string {
	for _, v := range labels {
		if bytes.Equal(v.Name, []byte("__name__")) {
			return string(transform.Normalize(v.Value))
		}
	}
	return string(transform.Normalize(labels[0].Value))
}
