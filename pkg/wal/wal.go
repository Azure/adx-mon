package wal

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/davidnarayan/go-flake"
)

// DefaultIOBufSize is the default buffer size for bufio.Writer.
const DefaultIOBufSize = 4 * 1024

var (
	ErrMaxDiskUsageExceeded   = fmt.Errorf("max disk usage exceeded")
	ErrMaxSegmentsExceeded    = fmt.Errorf("max segments exceeded")
	ErrMaxSegmentSizeExceeded = fmt.Errorf("max segment size exceeded")
	ErrSegmentClosed          = fmt.Errorf("segment closed")
	ErrSegmentLocked          = fmt.Errorf("segment locked")

	idgen *flake.Flake

	bwPool = pool.NewGeneric(10000, func(sz int) interface{} {
		return bufio.NewWriterSize(nil, 8*1024)
	})
)

func init() {
	var err error
	idgen, err = flake.New()
	if err != nil {
		panic(err)
	}
}

type WAL struct {
	opts WALOpts

	schemaPath string

	// index is the index of closed wal segments.  The active segment is not part of the index.
	index *Index

	sampleMetadataBuffer [12]byte

	closeFn context.CancelFunc

	wg      sync.WaitGroup
	mu      sync.RWMutex
	closed  bool
	segment Segment

	// inflightWriteBytes is the sum of bytes from goroutines with active writes in progress but not written
	// to the active segment.
	inflightWriteBytes int64

	// segmentSize tracks the size of the current segment.  This is tracked separately from Segment.Size() because
	// the latter requires taking an RLock on the segments which creates lock contention.
	segmentSize int64

	// segmentCreatedAt is the unixtime when the current segment was created.  This is tracked separately from Segment
	// itself to avoid lock contention.
	segmentCreatedAt int64
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

	// WALFlushInterval is the interval at which the WAL should be flushed.
	WALFlushInterval time.Duration

	// EnableWALFsync enables fsync of the segment after every flush.
	EnableWALFsync bool
}

type SampleType uint16

const (
	UnknownSampleType SampleType = iota
	MetricSampleType
	TraceSampleType
	LogSampleType
)

type WriteOptions func([]byte)

func NewWAL(opts WALOpts) (*WAL, error) {
	if opts.StorageDir == "" {
		return nil, fmt.Errorf("wal storage dir not defined")
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

	w.wg.Add(1)
	go w.rotate(ctx)

	return nil
}

func (w *WAL) Close() error {
	w.closeFn()

	w.wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true

	if w.segment != nil {
		info := w.segment.Info()
		if err := w.segment.Close(); err != nil {
			return err
		}

		w.index.Add(info)
		w.segment = nil
	}

	return nil
}

func (w *WAL) Write(ctx context.Context, buf []byte, opts ...WriteOptions) error {
	atomic.AddInt64(&w.inflightWriteBytes, int64(len(buf)))
	defer atomic.AddInt64(&w.inflightWriteBytes, -int64(len(buf)))

	// Optimistically try to write, but the segment might rotate in the meantime.
	// If it does, retry the write one more time.
	n, err := w.tryWrite(ctx, buf, opts...)
	if errors.Is(err, ErrMaxSegmentSizeExceeded) {
		w.rotateSegmentIfNecessary()
		n, err = w.tryWrite(ctx, buf)
		atomic.AddInt64(&w.segmentSize, int64(n))
		return err
	} else if errors.Is(err, ErrSegmentClosed) {
		n, err = w.tryWrite(ctx, buf, opts...)
		atomic.AddInt64(&w.segmentSize, int64(n))
		return err
	}
	atomic.AddInt64(&w.segmentSize, int64(n))
	return err
}

func (w *WAL) tryWrite(ctx context.Context, buf []byte, opts ...WriteOptions) (int, error) {
	var seg Segment
	if err := w.validateLimits(); err != nil {
		return 0, err
	}

	// fast path
	w.mu.RLock()
	if w.segment != nil {
		seg = w.segment
		w.mu.RUnlock()

		return seg.Write(ctx, buf, opts...)
	}
	w.mu.RUnlock()

	w.mu.Lock()
	if w.segment == nil {
		var err error
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix,
			WithFlushIntervale(w.opts.WALFlushInterval),
			WithFsync(w.opts.EnableWALFsync))
		if err != nil {
			w.mu.Unlock()
			return 0, err
		}
		w.segment = seg
	}
	seg = w.segment
	w.mu.Unlock()

	return seg.Write(ctx, buf, opts...)
}

func (w *WAL) validateLimits() error {
	if w.opts.MaxDiskUsage > 0 && w.index.TotalSize()+atomic.LoadInt64(&w.inflightWriteBytes) >= w.opts.MaxDiskUsage {
		return ErrMaxDiskUsageExceeded
	}

	if w.opts.MaxSegmentCount > 0 && w.index.TotalSegments() >= w.opts.MaxSegmentCount {
		return ErrMaxSegmentsExceeded
	}

	if w.opts.SegmentMaxSize > 0 && atomic.LoadInt64(&w.segmentSize)+atomic.LoadInt64(&w.inflightWriteBytes) >= w.opts.SegmentMaxSize {
		return ErrMaxSegmentSizeExceeded
	}

	return nil
}

func (w *WAL) Size() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.segment == nil {
		return 0
	}
	return int(w.segment.Size())
}

func (w *WAL) Segment() Segment {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.segment
}

func (w *WAL) rotate(ctx context.Context) {
	defer w.wg.Done()

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.rotateSegmentIfNecessary()
		}
	}
}

func (w *WAL) requiresRotation() bool {
	return (w.opts.SegmentMaxSize > 0 && atomic.LoadInt64(&w.segmentSize)+atomic.LoadInt64(&w.inflightWriteBytes) >= w.opts.SegmentMaxSize) ||
		(w.opts.SegmentMaxAge.Seconds() > 0 && time.Since(time.Unix(w.segmentCreatedAt, 0)) >= w.opts.SegmentMaxAge)
}

func (w *WAL) rotateSegmentIfNecessary() {
	if w.requiresRotation() {
		w.mu.Lock()
		// Re-verify rotation is needed under write lock since the fast path check is racy
		if !w.requiresRotation() {
			w.mu.Unlock()
			return
		}

		toClose := w.segment
		var err error
		w.segment, err = NewSegment(w.opts.StorageDir, w.opts.Prefix,
			WithFlushIntervale(w.opts.WALFlushInterval),
			WithFsync(w.opts.EnableWALFsync))
		if err != nil {
			logger.Errorf("Failed to create new segment: %s", err.Error())
			w.segment = nil
			atomic.StoreInt64(&w.segmentSize, 0)
			atomic.StoreInt64(&w.segmentCreatedAt, 0)
		} else {
			atomic.StoreInt64(&w.segmentSize, w.segment.Size())
			atomic.StoreInt64(&w.segmentCreatedAt, w.segment.CreatedAt().Unix())
		}
		w.mu.Unlock()

		if toClose != nil {
			// 8 bytes is the size of the segment magic header bytes.  If that is all we've written, we can just
			// delete it so that we don't end up uploading empty segments to Kusto.
			if toClose.Size() > 8 {
				info := toClose.Info()
				w.index.Add(info)
			} else {
				_ = os.Remove(toClose.Path())
			}

			if err := toClose.Close(); err != nil {
				logger.Errorf("Failed to close segment: %s %s", toClose.Path(), err.Error())
			}
		}
	}
}

// Path returns the path of the active segment.
func (w *WAL) Path() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.path()
}

func (w *WAL) path() string {
	if w.segment == nil {
		return ""
	}
	return w.segment.Path()
}

func (w *WAL) Remove(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (w *WAL) Append(ctx context.Context, buf []byte) error {
	atomic.AddInt64(&w.inflightWriteBytes, int64(len(buf)))
	defer atomic.AddInt64(&w.inflightWriteBytes, -int64(len(buf)))

	n, err := w.tryAppend(ctx, buf)
	if errors.Is(err, ErrMaxSegmentSizeExceeded) {
		w.rotateSegmentIfNecessary()
		n, err = w.tryAppend(ctx, buf)
		atomic.AddInt64(&w.segmentSize, int64(n))
		return err
	} else if errors.Is(err, ErrSegmentClosed) {
		n, err = w.tryAppend(ctx, buf)
		atomic.AddInt64(&w.segmentSize, int64(n))
		return err
	}
	atomic.AddInt64(&w.segmentSize, int64(n))
	return err
}

func (w *WAL) tryAppend(ctx context.Context, buf []byte) (int, error) {
	var seg Segment
	if err := w.validateLimits(); err != nil {
		return 0, err
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
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix,
			WithFlushIntervale(w.opts.WALFlushInterval),
			WithFsync(w.opts.EnableWALFsync))
		if err != nil {
			w.mu.Unlock()
			return 0, err
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

	closed := w.index.Get(nil, w.opts.Prefix)
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
