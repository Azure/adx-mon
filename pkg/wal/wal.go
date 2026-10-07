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

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
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

	// scheduler rotates the WAL's segments at their max age.  ownsScheduler is true when the WAL created it.
	scheduler     *rotationScheduler
	ownsScheduler bool

	// rotationIndex and rotationDeadline are guarded by scheduler.mu.  rotationIndex is -1 when not scheduled.
	rotationIndex    int
	rotationDeadline time.Time

	mu      sync.RWMutex
	closed  bool
	segment Segment

	// inflightWriteBytes is the sum of bytes from goroutines with active writes in progress but not written
	// to the active segment.
	inflightWriteBytes int64

	// segmentSize tracks the size of the current segment.  This is tracked separately from Segment.Size() because
	// the latter requires taking an RLock on the segments which creates lock contention.
	segmentSize int64

	// segmentCreatedAt is the creation time of the current segment in Unix nanoseconds, or 0 when there is no
	// current segment.  This is tracked separately from Segment itself to avoid lock contention.
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

	// Priority is the ingestion priority of the WAL's table.
	Priority ingestpolicy.Priority

	// scheduler is the shared rotation scheduler.  When nil, the WAL creates its own.
	scheduler *rotationScheduler
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
		index:         opts.Index,
		opts:          opts,
		scheduler:     opts.scheduler,
		rotationIndex: -1,
	}, nil
}

func (w *WAL) Open(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.scheduler == nil {
		w.scheduler = newRotationScheduler(defaultRotationSweepInterval, w.rotateSegmentIfNecessary)
		w.ownsScheduler = true
		w.scheduler.Open(context.Background())
	}

	return nil
}

func (w *WAL) Close() error {
	// Stop background rotations before closing the segment.  This must not hold w.mu since a rotation in progress
	// acquires it.
	if w.ownsScheduler {
		w.scheduler.Close()
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true

	seg := w.segment
	w.setSegment(nil)

	if seg != nil {
		info := seg.Info()
		if err := seg.Close(); err != nil {
			return err
		}

		w.index.Add(info)
	}

	return nil
}

// maxSegmentClosedRetries bounds how many times a write is retried when the segment it targeted was closed by a
// concurrent rotation.  A closed segment rejects the write without writing any data so retrying is safe.
const maxSegmentClosedRetries = 16

func (w *WAL) Write(ctx context.Context, buf []byte, opts ...WriteOptions) error {
	atomic.AddInt64(&w.inflightWriteBytes, int64(len(buf)))
	defer atomic.AddInt64(&w.inflightWriteBytes, -int64(len(buf)))

	return w.writeWithRetry(ctx, func() (int, error) {
		return w.tryWrite(ctx, buf, opts...)
	})
}

// writeWithRetry calls write, retrying when the segment rotates concurrently.  A write that exceeds the max segment
// size rotates the segment and is retried once.
func (w *WAL) writeWithRetry(ctx context.Context, write func() (int, error)) error {
	rotated := false
	for closedRetries := 0; ; {
		n, err := write()
		atomic.AddInt64(&w.segmentSize, int64(n))

		switch {
		case errors.Is(err, ErrMaxSegmentSizeExceeded) && !rotated:
			rotated = true
			w.rotateSegmentIfNecessary()
		case errors.Is(err, ErrSegmentClosed) && closedRetries < maxSegmentClosedRetries:
			closedRetries++
		default:
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}
	}
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
		seg, err := w.newSegment()
		if err != nil {
			w.mu.Unlock()
			return 0, err
		}
		w.setSegment(seg)
	}
	seg = w.segment
	w.mu.Unlock()

	return seg.Write(ctx, buf, opts...)
}

func (w *WAL) validateLimits() error {
	totalSize := w.index.TotalSize() + atomic.LoadInt64(&w.inflightWriteBytes)
	w.mu.RLock()
	if w.segment != nil {
		totalSize += w.segment.Size()
	}
	w.mu.RUnlock()

	if w.opts.MaxDiskUsage > 0 && totalSize >= w.opts.MaxDiskUsage {
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

// Priority returns the ingestion priority of the WAL's table.
func (w *WAL) Priority() ingestpolicy.Priority {
	return w.opts.Priority
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

func (w *WAL) requiresRotation() bool {
	if w.opts.SegmentMaxSize > 0 && atomic.LoadInt64(&w.segmentSize)+atomic.LoadInt64(&w.inflightWriteBytes) >= w.opts.SegmentMaxSize {
		return true
	}

	createdAt := atomic.LoadInt64(&w.segmentCreatedAt)
	return w.opts.SegmentMaxAge > 0 && createdAt != 0 && time.Since(time.Unix(0, createdAt)) >= w.opts.SegmentMaxAge
}

// newSegment creates a new segment for the WAL.
func (w *WAL) newSegment() (Segment, error) {
	return NewSegment(w.opts.StorageDir, w.opts.Prefix,
		WithFlushIntervale(w.opts.WALFlushInterval),
		WithFsync(w.opts.EnableWALFsync))
}

// setSegment sets the current segment and its tracked size and creation time, and schedules its rotation.  seg may
// be nil.  w.mu must be held for writing.
func (w *WAL) setSegment(seg Segment) {
	w.segment = seg
	if seg == nil {
		atomic.StoreInt64(&w.segmentSize, 0)
		atomic.StoreInt64(&w.segmentCreatedAt, 0)
		if w.scheduler != nil {
			w.scheduler.unschedule(w)
		}
		return
	}
	atomic.StoreInt64(&w.segmentSize, seg.Size())
	atomic.StoreInt64(&w.segmentCreatedAt, seg.CreatedAt().UnixNano())
	w.scheduleRotationLocked()
}

// scheduleRotation schedules rotation of the current segment at its max age.
func (w *WAL) scheduleRotation() {
	w.mu.RLock()
	defer w.mu.RUnlock()
	w.scheduleRotationLocked()
}

// scheduleRotationLocked is like scheduleRotation.  w.mu must be held.
func (w *WAL) scheduleRotationLocked() {
	if w.closed || w.scheduler == nil || w.opts.SegmentMaxAge <= 0 {
		return
	}
	createdAt := atomic.LoadInt64(&w.segmentCreatedAt)
	if createdAt == 0 {
		return
	}
	w.scheduler.schedule(w, time.Unix(0, createdAt).Add(w.opts.SegmentMaxAge))
}

func (w *WAL) rotateSegmentIfNecessary() {
	if w.requiresRotation() {
		w.mu.Lock()
		// Re-verify rotation is needed under write lock since the fast path check is racy
		if w.closed || !w.requiresRotation() {
			w.mu.Unlock()
			return
		}

		toClose := w.segment

		// Proactively create the next segment so writers do not create it while holding w.mu.  If the segment
		// being rotated is empty and no writes are in flight, the WAL has been idle for a full rotation period so
		// the next segment is created lazily by the next write instead.  This avoids repeatedly creating and
		// removing empty segments for idle WALs.
		var seg Segment
		idle := toClose != nil && toClose.Size() <= 8 && atomic.LoadInt64(&w.inflightWriteBytes) == 0
		if !idle {
			var err error
			seg, err = w.newSegment()
			if err != nil {
				logger.Errorf("Failed to create new segment: %s", err.Error())
				seg = nil
			}
		}
		w.setSegment(seg)
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

	return w.writeWithRetry(ctx, func() (int, error) {
		return w.tryAppend(ctx, buf)
	})
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
		seg, err := w.newSegment()
		if err != nil {
			w.mu.Unlock()
			return 0, err
		}
		w.setSegment(seg)
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
