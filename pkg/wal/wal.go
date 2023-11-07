package wal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	flakeutil "github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal/file"
)

var (
	ErrMaxDiskUsageExceeded = fmt.Errorf("max disk usage exceeded")
	ErrMaxSegmentsExceeded  = fmt.Errorf("max segments exceeded")
)

type WAL struct {
	opts WALOpts

	path       string
	schemaPath string

	// index is the index of closed wal segments.  The active segment is not part of the index.
	index *Index

	closeFn context.CancelFunc

	mu      sync.RWMutex
	closed  bool
	segment Segment
}

type SegmentInfo struct {
	Prefix    string
	Ulid      string
	Path      string
	Size      int64
	CreatedAt time.Time
}

type WALOpts struct {
	StorageDir string

	// StorageProvider is an implementation of the file.File interface.
	StorageProvider file.Provider

	// WAL segment prefix
	Prefix string

	// SegmentMaxSize is the max size of a segment in bytes before it will be rotated and compressed.
	SegmentMaxSize int64

	// SegmentMaxAge is the max age of a segment before it will be rotated and compressed.
	SegmentMaxAge time.Duration

	// MaxDiskUsage is the max disk usage of WAL segments allowed before writes should be rejected.
	MaxDiskUsage int64

	// MaxSegmentCount is the max number of segments allowed before writes should be rejected.
	MaxSegmentCount int

	// Index is the index of the WAL segments.
	Index *Index
}

func NewWAL(opts WALOpts) (*WAL, error) {
	if opts.StorageDir == "" && opts.StorageProvider == nil {
		return nil, fmt.Errorf("wal storage dir not defined")
	}
	if opts.StorageProvider == nil {
		opts.StorageProvider = &file.DiskProvider{}
	}

	if opts.Index == nil {
		opts.Index = NewIndex()
	}

	return &WAL{
		index: opts.Index,
		opts:  opts,
	}, nil
}

func (w *WAL) Open(ctx context.Context) error {
	ctx, w.closeFn = context.WithCancel(context.Background())
	w.mu.Lock()
	defer w.mu.Unlock()

	// Loading existing segments is not supported for memory provider
	if _, ok := w.opts.StorageProvider.(*file.MemoryProvider); ok {
		return nil
	}

	dir, err := w.opts.StorageProvider.Open(w.opts.StorageDir)
	if err != nil {
		return err
	}

	for {
		entries, err := dir.ReadDir(100)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		for _, d := range entries {
			path := filepath.Join(w.opts.StorageDir, d.Name())
			if d.IsDir() || filepath.Ext(path) != ".wal" {
				return nil
			}

			fileName := filepath.Base(path)
			if bytes.HasPrefix([]byte(fileName), []byte(w.opts.Prefix)) {
				fi, err := d.Info()
				if err != nil {
					logger.Warnf("Failed to get file info: %s %s", path, err.Error())
					continue
				}

				fields := strings.Split(fileName, "_")
				epoch := fields[len(fields)-1][:len(fields[len(fields)-1])-4]

				createdAt, err := flakeutil.ParseFlakeID(epoch)
				if err != nil {
					logger.Warnf("Failed to parse flake id: %s %s", epoch, err.Error())
					continue
				}

				info := SegmentInfo{
					Prefix:    w.opts.Prefix,
					Ulid:      epoch,
					Path:      path,
					Size:      fi.Size(),
					CreatedAt: createdAt,
				}
				w.index.Add(info)
			}
		}
	}

	go w.rotate(ctx)

	return nil
}

func (w *WAL) Close() error {
	w.closeFn()

	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true

	if w.segment != nil {
		info, err := w.segment.Info()
		if err != nil {
			return err
		}

		if err := w.segment.Close(); err != nil {
			return err
		}

		w.index.Add(info)
		w.segment = nil
	}

	return nil
}

func (w *WAL) Write(ctx context.Context, buf []byte) error {
	var seg Segment

	if err := w.validateLimits(buf); err != nil {
		return err
	}

	// fast path
	w.mu.RLock()
	if w.segment != nil {
		seg = w.segment
		w.mu.RUnlock()

		return seg.Write(ctx, buf)

	}
	w.mu.RUnlock()

	w.mu.Lock()
	if w.segment == nil {
		var err error
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix, w.opts.StorageProvider)
		if err != nil {
			w.mu.Unlock()
			return err
		}
		w.segment = seg
	}
	seg = w.segment
	w.mu.Unlock()

	return seg.Write(ctx, buf)
}

func (w *WAL) validateLimits(buf []byte) error {
	if w.opts.MaxDiskUsage > 0 && w.index.TotalSize()+int64(len(buf)) > w.opts.MaxDiskUsage {
		return ErrMaxDiskUsageExceeded
	}

	if w.opts.MaxSegmentCount > 0 && w.index.TotalSegments() >= w.opts.MaxSegmentCount {
		return ErrMaxSegmentsExceeded
	}
	return nil
}

func (w *WAL) Size() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.segment == nil {
		return 0
	}
	return 1
}

func (w *WAL) Segment() Segment {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.segment
}

func (w *WAL) rotate(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:

			w.mu.Lock()
			if w.segment == nil {
				w.mu.Unlock()
				continue
			}

			var toClose Segment
			seg := w.segment
			sz, err := seg.Size()
			if err != nil {
				w.mu.Unlock()
				logger.Errorf("Failed segment size: %s %s", seg.Path(), err.Error())
				continue
			}

			// Rotate the segment once it's past the max segment size
			if (w.opts.SegmentMaxSize != 0 && sz >= w.opts.SegmentMaxSize) ||
				(w.opts.SegmentMaxAge.Seconds() != 0 && time.Since(seg.CreatedAt()).Seconds() > w.opts.SegmentMaxAge.Seconds()) {
				toClose = seg
				w.segment = nil
			}
			w.mu.Unlock()

			if toClose != nil {
				info, err := toClose.Info()
				if err != nil {
					logger.Errorf("Failed to get segment info: %s %s", toClose.Path(), err.Error())
				} else {
					w.index.Add(info)
				}

				if err := toClose.Close(); err != nil {
					logger.Errorf("Failed to close segment: %s %s", toClose.Path(), err.Error())
				}
			}
		}

	}
}

// Path returns the path of the active segment.
func (w *WAL) Path() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.segment == nil {
		return ""
	}

	return w.segment.Path()
}

func (w *WAL) Remove(path string) error {
	return os.Remove(path)
}

func (w *WAL) Append(ctx context.Context, buf []byte) error {
	var seg Segment

	if err := w.validateLimits(buf); err != nil {
		return err
	}

	// fast path
	w.mu.RLock()
	if w.segment != nil {
		seg = w.segment
		w.mu.RUnlock()

		return seg.Append(ctx, buf)

	}
	w.mu.RUnlock()

	w.mu.Lock()
	if w.segment == nil {
		var err error
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix, w.opts.StorageProvider)
		if err != nil {
			w.mu.Unlock()
			return err
		}
		w.segment = seg
	}
	seg = w.segment
	w.mu.Unlock()

	return seg.Append(ctx, buf)
}

func (w *WAL) RemoveAll() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.closed {
		return fmt.Errorf("wal not closed")
	}

	closed := w.index.Get(w.opts.Prefix)
	for _, info := range closed {
		if err := w.Remove(info.Path); err != nil {
			return err
		}
		w.index.Remove(info)
	}

	if w.segment != nil {
		return w.Remove(w.segment.Path())
	}
	return nil
}

func (w *WAL) Flush() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.segment == nil {
		return nil
	}

	return w.segment.Flush()
}
