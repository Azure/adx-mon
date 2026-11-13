package wal

import (
	"bufio"
	"container/list"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
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

	bwPool = pool.NewGeneric(10000, func(sz int) any {
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

	// Lease state is guarded by repository.lifecycle. Standalone WALs need no lease.
	repository *Repository
	users      int
	idleSince  time.Time
	idleEntry  *list.Element

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

	// rotateMu serializes rotations so writers that fill the segment together create one next segment rather than
	// one each.
	rotateMu sync.Mutex

	// inflightWriteBytes is the sum of bytes from goroutines with active writes in progress but not written
	// to the active segment.
	inflightWriteBytes atomic.Int64

	// maxAge is the segment age limit in nanoseconds.  It starts at opts.SegmentMaxAge and changes with
	// SetSegmentMaxAge.
	maxAge atomic.Int64

	// rotationJitter is how much earlier than its max age the current segment rotates.  WALs whose segments are
	// created together, such as at startup or by one rotation sweep, would otherwise keep rotating at the same moment.
	rotationJitter atomic.Int64

	// segmentSize tracks the size of the current segment.  This is tracked separately from Segment.Size() because
	// the latter requires taking an RLock on the segments which creates lock contention.
	segmentSize atomic.Int64

	// segmentCreatedAt is when the current segment became active, in Unix nanoseconds on the monotonic clock (see
	// monoNow), or 0 when there is no segment.  Rotation ages use it rather than the segment's wall clock creation
	// time so a clock step does not delay or advance rotations.
	segmentCreatedAt int64
}

type SegmentInfo struct {
	Prefix    string
	Ulid      string
	Path      string
	Size      int64
	CreatedAt time.Time

	// RawSize is the uncompressed size of the segment's values, as they are ingested, or zero when it is not known.
	RawSize int64

	// Priority is the ingestion priority of the segment's table.
	Priority ingestpolicy.Priority
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

	// Admission owns per-destination capacity accounting.  When nil, table
	// budgets are not applied to this WAL.
	Admission *Admission
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

	w := &WAL{
		index:         opts.Index,
		opts:          opts,
		scheduler:     opts.scheduler,
		rotationIndex: -1,
	}
	w.maxAge.Store(int64(opts.SegmentMaxAge))
	return w, nil
}

// SegmentMaxAge returns the age at which the WAL's segments rotate.  0 means they do not rotate by age.
func (w *WAL) SegmentMaxAge() time.Duration {
	return time.Duration(w.maxAge.Load())
}

// SetSegmentMaxAge changes the age at which the WAL's segments rotate, including the current segment.  0 stops
// rotating them by age.
func (w *WAL) SetSegmentMaxAge(d time.Duration) {
	if w.SegmentMaxAge() == d {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.maxAge.Store(int64(d))
	w.rotationJitter.Store(int64(rotationJitter(d)))
	if d <= 0 {
		if w.scheduler != nil {
			w.scheduler.unschedule(w)
		}
		return
	}
	w.scheduleRotationLocked()
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
		info.Priority = w.opts.Priority
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
	w.inflightWriteBytes.Add(int64(len(buf)))
	defer w.inflightWriteBytes.Add(-int64(len(buf)))

	return w.writeWithRetry(ctx, func() (int, error) {
		return w.tryWrite(ctx, buf, opts...)
	})
}

// writeWithRetry calls write, retrying when the segment rotates concurrently.  A write that exceeds the max segment
// size rotates the segment and is retried, up to maxSegmentClosedRetries times since concurrent writers can fill
// the next segment first.
//
// A write rejected by a table budget releases the active segment: the uploader
// only drains closed segments, so otherwise a throttled table whose segment has
// not reached its rotation size or age would never free capacity and could stay
// throttled indefinitely.
func (w *WAL) writeWithRetry(ctx context.Context, write func() (int, error)) error {
	rotations := 0
	for closedRetries := 0; ; {
		n, err := write()
		w.segmentSize.Add(int64(n))

		switch {
		case errors.Is(err, ErrMaxSegmentSizeExceeded) && rotations < maxSegmentClosedRetries:
			rotations++
			w.rotateSegmentIfNecessary()
		case errors.Is(err, ErrSegmentClosed) && closedRetries < maxSegmentClosedRetries:
			closedRetries++
		default:
			if _, ok := errors.AsType[*TableOverloadedError](err); ok {
				w.closeActiveSegment()
			}
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// closeActiveSegment closes and indexes the active segment, if any, so its
// capacity can be reclaimed once it is uploaded.
func (w *WAL) closeActiveSegment() {
	w.mu.Lock()
	toClose := w.segment
	if toClose == nil {
		w.mu.Unlock()
		return
	}
	w.setSegment(nil)
	w.mu.Unlock()

	// Closed before indexing so the segment is fully written when it is dispatched.  A failed close still indexes
	// it so the backlog stays discoverable by the uploader rather than stranded until restart.
	if err := toClose.Close(); err != nil {
		logger.Errorf("Failed to close throttled segment: %s %s", toClose.Path(), err.Error())
	}

	if toClose.Size() > 8 {
		info := toClose.Info()
		info.Priority = w.opts.Priority
		w.index.Add(info)
	} else {
		_ = w.Remove(toClose.Path())
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
	if w.closed {
		w.mu.Unlock()
		return 0, ErrSegmentClosed
	}
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
	if w.opts.Admission != nil {
		if w.opts.SegmentMaxSize > 0 && w.segmentSize.Load() > 8 && w.segmentSize.Load()+w.inflightWriteBytes.Load() >= w.opts.SegmentMaxSize {
			return ErrMaxSegmentSizeExceeded
		}
		return nil
	}
	totalSize := w.index.TotalSize() + w.inflightWriteBytes.Load()
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

	if w.opts.SegmentMaxSize > 0 && w.segmentSize.Load() > 8 && w.segmentSize.Load()+w.inflightWriteBytes.Load() >= w.opts.SegmentMaxSize {
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
	if w.opts.SegmentMaxSize > 0 && w.segmentSize.Load()+w.inflightWriteBytes.Load() >= w.opts.SegmentMaxSize {
		return true
	}

	createdAt := atomic.LoadInt64(&w.segmentCreatedAt)
	return w.SegmentMaxAge() > 0 && createdAt != 0 && time.Duration(monoNow()-createdAt) >= w.segmentMaxAge()
}

// processStart anchors segment ages to the monotonic clock.
var processStart = time.Now()

// monoNow returns the current time in Unix nanoseconds, measured on the monotonic clock since processStart so it
// does not move when the wall clock is stepped.
func monoNow() int64 {
	return processStart.UnixNano() + int64(time.Since(processStart))
}

// monoTime returns the time for a monoNow value with a monotonic reading, so timers and comparisons against it
// also ignore wall clock steps.
func monoTime(ns int64) time.Time {
	return processStart.Add(time.Duration(ns - processStart.UnixNano()))
}

// rotationJitterFraction is the largest fraction of the max segment age by which a segment rotates early.
const rotationJitterFraction = 10

// segmentMaxAge returns the age at which the current segment rotates: its max age less its jitter.
func (w *WAL) segmentMaxAge() time.Duration {
	return w.SegmentMaxAge() - time.Duration(w.rotationJitter.Load())
}

// rotationJitter returns a random duration up to a tenth of maxAge by which a segment rotates early.
func rotationJitter(maxAge time.Duration) time.Duration {
	if window := maxAge / rotationJitterFraction; window > 0 {
		return rand.N(window)
	}
	return 0
}

// testHookSegmentCreated, if set, is called each time a WAL creates a segment.
var testHookSegmentCreated func()

// newSegment creates a new segment for the WAL.
func (w *WAL) newSegment() (Segment, error) {
	if testHookSegmentCreated != nil {
		testHookSegmentCreated()
	}
	return NewSegment(w.opts.StorageDir, w.opts.Prefix,
		WithFlushIntervale(w.opts.WALFlushInterval),
		WithFsync(w.opts.EnableWALFsync),
		WithAdmission(w.opts.Admission))
}

// setSegment sets the current segment and its tracked size and creation time, and schedules its rotation.  seg may
// be nil.  w.mu must be held for writing.
func (w *WAL) setSegment(seg Segment) {
	w.segment = seg
	if seg == nil {
		w.segmentSize.Store(0)
		atomic.StoreInt64(&w.segmentCreatedAt, 0)
		if w.scheduler != nil {
			w.scheduler.unschedule(w)
		}
		return
	}
	w.segmentSize.Store(seg.Size())
	atomic.StoreInt64(&w.segmentCreatedAt, monoNow())
	w.rotationJitter.Store(int64(rotationJitter(w.SegmentMaxAge())))
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
	if w.closed || w.scheduler == nil || w.SegmentMaxAge() <= 0 {
		return
	}
	createdAt := atomic.LoadInt64(&w.segmentCreatedAt)
	if createdAt == 0 {
		return
	}
	w.scheduler.schedule(w, monoTime(createdAt).Add(w.segmentMaxAge()))
}

func (w *WAL) rotateSegmentIfNecessary() {
	if !w.requiresRotation() {
		return
	}

	// A rotation that finished while waiting resets the segment size, so the check below returns early.
	w.rotateMu.Lock()
	defer w.rotateMu.Unlock()
	if !w.requiresRotation() {
		return
	}

	w.mu.RLock()
	cur, closed := w.segment, w.closed
	w.mu.RUnlock()
	if closed {
		return
	}

	// Proactively create the next segment so writers do not create it while holding w.mu.  It is created before
	// acquiring w.mu so writers are not blocked on file creation during rotation.  If the WAL is idle, the next
	// segment is created lazily by the next write instead to avoid repeatedly creating and removing empty segments.
	var next Segment
	if !w.isIdle(cur) {
		next = w.createNextSegment()
	}

	w.mu.Lock()
	// Re-verify rotation is needed under write lock since the segment may have been rotated concurrently.
	if w.closed || w.segment != cur || !w.requiresRotation() {
		w.mu.Unlock()
		w.discardSegment(next)
		return
	}

	// A write may have arrived after the WAL was considered idle.  Keep the next segment proactive in that case.
	if next == nil && !w.isIdle(cur) {
		next = w.createNextSegment()
	}

	toClose := cur
	w.setSegment(next)
	w.mu.Unlock()

	if toClose != nil {
		// Closed before indexing so the segment is fully written when it is dispatched.
		if err := toClose.Close(); err != nil {
			logger.Errorf("Failed to close segment: %s %s", toClose.Path(), err.Error())
		}

		// 8 bytes is the size of the segment magic header bytes.  If that is all we've written, we can just
		// delete it so that we don't end up uploading empty segments to Kusto.
		if toClose.Size() > 8 {
			info := toClose.Info()
			info.Priority = w.opts.Priority
			w.index.Add(info)
		} else {
			// Removed through the WAL so the table budget is released.
			_ = w.Remove(toClose.Path())
		}
	}
}

// isIdle returns true if seg is empty and no writes are in flight, meaning the WAL received no writes for a full
// rotation period.
func (w *WAL) isIdle(seg Segment) bool {
	return seg != nil && seg.Size() <= 8 && w.inflightWriteBytes.Load() == 0
}

// createNextSegment creates the next segment, logging and returning nil on failure so the next write creates it.
func (w *WAL) createNextSegment() Segment {
	seg, err := w.newSegment()
	if err != nil {
		logger.Errorf("Failed to create new segment: %s", err.Error())
		return nil
	}
	return seg
}

// discardSegment closes and removes an unused segment.  It is removed through the WAL so the admission reservation
// taken when the segment was created is released; otherwise the ingestor would keep accounting for a file that no
// longer exists.
func (w *WAL) discardSegment(seg Segment) {
	if seg == nil {
		return
	}
	if err := seg.Close(); err != nil {
		logger.Errorf("Failed to close segment: %s %s", seg.Path(), err.Error())
	}
	if err := w.Remove(seg.Path()); err != nil {
		logger.Errorf("Failed to remove segment: %s %s", seg.Path(), err.Error())
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
	if err == nil || os.IsNotExist(err) {
		if d, derr := DestinationFromPrefix(w.opts.Prefix); derr == nil {
			w.opts.Admission.Forget(d, path)
		}
		return nil
	}
	return err
}

func (w *WAL) Append(ctx context.Context, buf []byte) error {
	w.inflightWriteBytes.Add(int64(len(buf)))
	defer w.inflightWriteBytes.Add(-int64(len(buf)))

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
	if w.closed {
		w.mu.Unlock()
		return 0, ErrSegmentClosed
	}
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
