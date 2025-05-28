package wal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/partmap"
)

// Repository is a collection of WALs.
type Repository struct {
	opts RepositoryOpts

	index *Index

	wals *partmap.Map[*WAL]
}

type RepositoryOpts struct {
	StorageDir     string
	SegmentMaxSize int64
	SegmentMaxAge  time.Duration

	MaxDiskUsage     int64
	MaxSegmentCount  int
	WALFlushInterval time.Duration
	EnableWALFsync   bool
}

func NewRepository(opts RepositoryOpts) *Repository {
	return &Repository{
		opts:  opts,
		index: NewIndex(),
		wals:  partmap.NewMap[*WAL](64),
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

	dir, err := os.Open(s.opts.StorageDir)
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

			// This block was added when we had an non-backwards compatible segment file change in the segment
			// file format.  To simplify the migration, we just remove any segment files that are not in the
			// expected format.  In the future, we will have a versioned segment file format to avoid this.
			if !IsSegment(path) {
				logger.Warnf("Segment file is not a WAL segment file: %s. Removing", path)
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					logger.Warnf("Failed to remove invalid segment file: %s %s", path, err.Error())
				}
				continue
			}

			// Walk each segment on disk and Open it to trigger a repair if necessary.
			var seg Segment
			seg, err = Open(path)
			if err != nil {
				logger.Warnf("Failed to open segment file: %s %s", path, err.Error())
			} else if err := seg.Close(); err != nil {
				logger.Warnf("Failed to close segment file: %s %s", path, err.Error())
			}

			fileName := filepath.Base(path)
			database, table, schema, epoch, err := ParseFilename(fileName)
			if err != nil {
				continue
			}

			prefix := fmt.Sprintf("%s_%s", database, table)
			if schema != "" {
				prefix = fmt.Sprintf("%s_%s", prefix, schema)
			}

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

			// If the segment is only 8 bytes, that means only the segment magic header has been written and there
			// is no data in the file.  We don't want to upload these to kusto so they can be removed.
			if fi.Size() == 8 {
				if logger.IsDebug() {
					logger.Debugf("Removing empty segment: %s", path)
				}
				if err := os.Remove(path); err != nil {
					logger.Warnf("Failed to remove empty segment: %s %s", path, err.Error())
				}
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
	if err := s.wals.Each(func(key string, value *WAL) error {
		wal := value
		return wal.Close()
	}); err != nil {
		return err
	}

	return nil
}

func (s *Repository) newWAL(ctx context.Context, prefix string) (*WAL, error) {
	walOpts := WALOpts{
		Prefix:           prefix,
		StorageDir:       s.opts.StorageDir,
		SegmentMaxSize:   s.opts.SegmentMaxSize,
		SegmentMaxAge:    s.opts.SegmentMaxAge,
		MaxDiskUsage:     s.opts.MaxDiskUsage,
		Index:            s.index,
		WALFlushInterval: s.opts.WALFlushInterval,
		EnableWALFsync:   s.opts.EnableWALFsync,
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
	v, err := s.wals.GetOrCreate(string(key), func() (*WAL, error) {
		return s.newWAL(ctx, string(key))
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Repository) Count() int {
	return s.wals.Count()
}

func (s *Repository) Remove(key []byte) error {
	wal, ok := s.wals.Get(string(key))
	if !ok {
		return nil
	}

	w := wal

	if err := w.RemoveAll(); err != nil {
		return err
	}

	s.wals.Delete(string(key))
	return nil
}

func (s *Repository) Keys() [][]byte {
	keys := make([][]byte, 0, s.wals.Count())
	s.wals.Each(func(key string, value *WAL) error {
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

func (s *Repository) RemoveSegment(si SegmentInfo) {
	s.index.Remove(si)
}

func (s *Repository) PrefixesByAge() []string {
	return s.index.PrefixesByAge()
}

func (s *Repository) WriteDebug(w io.Writer) error {
	if err := s.index.WriteDebug(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 4, 0, 2, ' ', 0)
	_, _ = tw.Write([]byte("Prefix\tPath\tDisk Usage\n"))

	var walsSize, count int
	if err := s.wals.Each(func(key string, value *WAL) error {
		walsSize += value.Size()
		count++
		tw.Write([]byte(fmt.Sprintf("%s\t%s\t%d\n", key, value.Path(), value.Size())))
		return nil
	}); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\nWAL: Disk Usage: %d, Segments: %d\n", walsSize, count)
	return tw.Flush()
}

// ActiveSegmentsSize returns the total size of all active segments
func (s *Repository) ActiveSegmentsSize() int64 {
	var totalSize int64
	s.wals.Each(func(key string, value *WAL) error {
		totalSize += int64(value.Size())
		return nil
	})
	return totalSize
}

// ActiveSegmentsTotal returns the total count of all active segments
func (s *Repository) ActiveSegmentsTotal() int64 {
	return int64(s.wals.Count())
}
