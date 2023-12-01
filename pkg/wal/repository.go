package wal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/Azure/adx-mon/pkg/wal/file"
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
	if s.opts.StorageDir == "" {
		return fmt.Errorf("storage dir is required")
	}

	stat, err := os.Stat(s.opts.StorageDir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(s.opts.StorageDir, 0755); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !stat.IsDir() {
		return fmt.Errorf("storage dir is not a directory")
	}

	// Loading existing segments is not supported for memory provider
	if _, ok := s.opts.StorageProvider.(*file.MemoryProvider); ok {
		return nil
	}

	dir, err := s.opts.StorageProvider.Open(s.opts.StorageDir)
	if err != nil {
		return err
	}

	for {
		entries, err := dir.ReadDir(100)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		for _, d := range entries {
			path := filepath.Join(s.opts.StorageDir, d.Name())
			if d.IsDir() || filepath.Ext(path) != ".wal" {
				continue
			}

			fileName := filepath.Base(path)
			fields := strings.Split(fileName, "_")
			if len(fields) != 3 || fields[0] == "" {
				continue
			}

			database := fields[0]
			table := fields[1]
			epoch := fields[2][:len(fields[2])-4]
			prefix := fmt.Sprintf("%s_%s", database, table)

			createdAt, err := flakeutil.ParseFlakeID(epoch)
			if err != nil {
				logger.Warnf("Failed to parse flake id: %s %s", epoch, err.Error())
				continue
			}

			fi, err := d.Info()
			if err != nil {
				logger.Warnf("Failed to get file info: %s %s", path, err.Error())
				continue
			}

			info := SegmentInfo{
				Prefix:    prefix,
				Ulid:      epoch,
				Path:      path,
				Size:      fi.Size(),
				CreatedAt: createdAt,
			}
			s.index.Add(info)

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

		walPath := wal.Path()
		if walPath != "" && walPath == path {
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
		walPath := wal.Path()
		if walPath != "" && walPath == filename {
			exists = true
		}
		return nil
	})
	return exists
}

func (s *Repository) GetSegments(prefix string) []SegmentInfo {
	return s.index.Get(prefix)
}

func (s *Repository) RemoveSegment(si SegmentInfo) {
	s.index.Remove(si)
}

func (s *Repository) PrefixesByAge() []string {
	return s.index.PrefixesByAge()
}
