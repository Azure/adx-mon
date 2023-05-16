package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
)

type WAL struct {
	opts WALOpts

	path       string
	schemaPath string

	closeFn context.CancelFunc

	mu      sync.RWMutex
	segment Segment
}

type WALOpts struct {
	StorageDir string

	// WAL segment prefix
	Prefix string

	// SegmentMaxSize is the max size of a segment in bytes before it will be rotated and compressed.
	SegmentMaxSize int64

	// SegmentMaxAge is the max age of a segment before it will be rotated and compressed.
	SegmentMaxAge time.Duration
}

func NewWAL(opts WALOpts) (*WAL, error) {
	if opts.StorageDir == "" {
		return nil, fmt.Errorf("wal storage dir not defined")
	}

	return &WAL{
		opts: opts,
	}, nil
}

func (w *WAL) Open(ctx context.Context) error {
	ctx, w.closeFn = context.WithCancel(context.Background())
	w.mu.Lock()
	defer w.mu.Unlock()

	go w.rotate(ctx)

	return nil
}

func (w *WAL) Close() error {
	w.closeFn()

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.segment != nil {
		if err := w.segment.Close(); err != nil {
			return err
		}
		w.segment = nil
	}

	return nil
}

func (w *WAL) Write(ctx context.Context, ts []prompb.TimeSeries) error {
	var seg Segment

	// fast path
	w.mu.RLock()
	if w.segment != nil {
		seg = w.segment
		w.mu.RUnlock()

		return seg.Write(ctx, ts)

	}
	w.mu.RUnlock()

	w.mu.Lock()
	if w.segment == nil {
		var err error
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix)
		if err != nil {
			w.mu.Unlock()
			return err
		}
		w.segment = seg
	}
	seg = w.segment
	w.mu.Unlock()

	return seg.Write(ctx, ts)
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
				logger.Error("Failed segment size: %s %s", seg.Path(), err.Error())
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
				if err := toClose.Close(); err != nil {
					logger.Error("Failed to close segment: %s %s", toClose.Path(), err.Error())
				}
			}
		}

	}
}

func (w *WAL) SegmentPath() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.segment == nil {
		return ""
	}

	return w.segment.Path()
}
