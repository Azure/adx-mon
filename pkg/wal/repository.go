package wal

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/wal/file"
)

var (
	bytesPool = pool.NewBytes(1000)
)

// Repository is a collection of WALs.
type Repository struct {
	opts RepositoryOpts

	index *Index

	wals *partmap.Map
}

type RepositoryOpts struct {
	StorageDir     string
	SegmentMaxSize int64
	SegmentMaxAge  time.Duration

	MaxDiskUsage    int64
	MaxSegmentCount int

	// StorageProvider is an implementation of the file.File interface.
	StorageProvider file.Provider
}

func NewRepository(opts RepositoryOpts) *Repository {
	return &Repository{
		opts:  opts,
		index: NewIndex(),
		wals:  partmap.NewMap(64),
	}
}

func (s *Repository) Open(ctx context.Context) error {
	wals, err := ListDir(s.opts.StorageDir)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", s.opts.StorageDir, err)
	}
	for _, w := range wals {
		prefix := fmt.Sprintf("%s_%s", w.Database, w.Table)
		_, ok := s.wals.Get(prefix)
		if ok {
			continue
		}

		wal, err := s.newWAL(ctx, prefix)
		if err != nil {
			return err
		}
		s.wals.Set(prefix, wal)
	}
	return nil
}

func (s *Repository) Close() error {
	if err := s.wals.Each(func(key string, value any) error {
		wal := value.(*WAL)
		return wal.Close()
	}); err != nil {
		return err
	}

	return nil
}

func (s *Repository) newWAL(ctx context.Context, prefix string) (*WAL, error) {
	walOpts := WALOpts{
		Prefix:          prefix,
		StorageDir:      s.opts.StorageDir,
		StorageProvider: s.opts.StorageProvider,
		SegmentMaxSize:  s.opts.SegmentMaxSize,
		SegmentMaxAge:   s.opts.SegmentMaxAge,
		Index:           s.index,
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

func (s *Repository) Get(ctx context.Context, key []byte) (*WAL, error) {
	v, err := s.wals.GetOrCreate(string(key), func() (any, error) {
		return s.newWAL(ctx, string(key))
	})
	if err != nil {
		return nil, err
	}
	return v.(*WAL), nil
}

func (s *Repository) Count() int {
	return s.wals.Count()
}

func (s *Repository) Remove(key []byte) error {
	wal, ok := s.wals.Get(string(key))
	if !ok {
		return nil
	}

	w := wal.(*WAL)

	if err := w.RemoveAll(); err != nil {
		return err
	}

	s.wals.Delete(string(key))
	return nil
}

func (s *Repository) Keys() [][]byte {
	keys := make([][]byte, 0, s.wals.Count())
	s.wals.Each(func(key string, value any) error {
		keys = append(keys, []byte(key))
		return nil
	})
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i], keys[j]) < 0
	})
	return keys
}

func (s *Repository) Index() *Index {
	return s.index
}

func (s *Repository) IsActiveSegment(path string) bool {
	var active bool
	s.wals.Each(func(key string, value any) error {
		wal := value.(*WAL)
		if wal.path == path {
			active = true
		}
		return nil
	})
	return active
}

func (s *Repository) SegmentExists(filename string) bool {
	if s.index.SegmentExists(filename) {
		return true
	}

	var exists bool
	s.wals.Each(func(key string, value any) error {
		wal := value.(*WAL)
		if wal.path == filename {
			exists = true
		}
		return nil
	})
	return exists
}
