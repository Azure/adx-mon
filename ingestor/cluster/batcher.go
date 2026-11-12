package cluster

import (
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
	"github.com/prometheus/client_golang/prometheus"
)

// DefaultMaxSegmentCount is the default maximum number of segments to include in a batch before creating a new batch.
const DefaultMaxSegmentCount = 25

type Segmenter interface {
	Get(infos []wal.SegmentInfo, prefix string) []wal.SegmentInfo
	PrefixesByAge() []string
	Remove(si wal.SegmentInfo)
	TotalSegments() int
	TotalSize() int64
	OldestSegmentAge() time.Duration
}

// SegmentUsage reports the number and total size of WAL segment files, both active and closed.
type SegmentUsage interface {
	SegmentCount() int64
	Size() int64
}

type BatcherOpts struct {
	StorageDir       string
	MinUploadSize    int64
	MaxSegmentAge    time.Duration
	MaxTransferSize  int64
	MaxTransferAge   time.Duration
	MaxBatchSegments int
	MaxTableBatches  int

	Partitioner MetricPartitioner
	Segmenter   Segmenter

	UploadQueue        chan *Batch
	TransferQueue      chan *Batch
	PeerHealthReporter PeerHealthReporter

	// RealtimeUploadQueue and RealtimeTransferQueue receive realtime batches.  When nil, realtime batches are sent to
	// UploadQueue and TransferQueue.
	RealtimeUploadQueue   chan *Batch
	RealtimeTransferQueue chan *Batch

	// Realtime enables event driven batching of realtime segments.  When disabled, realtime segments are batched by
	// the periodic scan.
	Realtime RealtimeBatchOpts

	// QueuedLinger is the maximum time a closed queued segment waits to be batched with others.  Defaults to
	// DefaultQueuedLinger.
	QueuedLinger time.Duration

	// TransferLinger, when positive, replaces QueuedLinger for segments of tables owned by another node.  Their
	// batches are merged while they wait for a transfer worker, so they can be sent sooner without making more,
	// smaller transfers.
	TransferLinger time.Duration

	// MaxRetryDelay caps the backoff of a table whose batches keep failing.  Defaults to the queued sweep interval, a
	// minute.
	MaxRetryDelay time.Duration

	TransfersDisabled bool

	// Usage is optional.  When set, SegmentsTotal and SegmentsSize, and the metrics derived from them, report every
	// segment file the store accounts for, including active segments, rather than only the closed segments in the
	// Segmenter.  This keeps the metrics consistent with the health status and with what is on disk.
	Usage SegmentUsage

	// Metrics for observability
	SegmentsCountMetric     prometheus.Gauge
	SegmentsSizeBytesMetric prometheus.Gauge
	SegmentsMaxAgeMetric    prometheus.Gauge

	// SegmentsSizeByPriorityMetric is optional.  When set, it reports segment sizes by ingestion priority if the
	// Segmenter tracks them.
	SegmentsSizeByPriorityMetric *prometheus.GaugeVec
}

// prioritySizer is implemented by segmenters that track segment sizes by ingestion priority.
type prioritySizer interface {
	TotalSizeByPriority(p ingestpolicy.Priority) int64
}

type Batch struct {
	Segments []wal.SegmentInfo
	Database string
	Table    string
	Prefix   string

	// Priority is the ingestion priority of the batch's table.
	Priority ingestpolicy.Priority

	batcher Batcher

	released   bool
	removed    bool
	mu         sync.Mutex
	completion func(time.Duration)
	retryDelay time.Duration
	// queueFull is set when the batch is released unsent because its destination's queue was full.
	queueFull bool
	// done, when set, is called once when the batch is finished: released, or merged into another batch.
	done     func()
	finished bool
}

// finish calls the batch's done hook the first time the batch is released or merged into another batch.
func (b *Batch) finish() {
	b.mu.Lock()
	if b.finished {
		b.mu.Unlock()
		return
	}
	b.finished = true
	done := b.done
	b.mu.Unlock()
	if done != nil {
		done()
	}
}

// Release releases the segments in the batch so they can be processed again.
func (b *Batch) Release() {
	b.mu.Lock()
	if b.released {
		b.mu.Unlock()
		return
	}
	b.released = true
	completion, delay := b.completion, b.retryDelay
	b.mu.Unlock()
	if b.batcher != nil {
		b.batcher.Release(b)
	}
	b.finish()
	if completion != nil {
		completion(delay)
	}
}

// releaseQueueFull releases a batch that was not sent because its destination's queue was full.  Unlike a failed
// delivery, it says nothing about whether the destination is reachable.
func (b *Batch) releaseQueueFull() {
	b.mu.Lock()
	b.queueFull = true
	b.mu.Unlock()
	b.Release()
}

// RetryAfter releases the worker promptly while deferring this table's next work.
func (b *Batch) RetryAfter(delay time.Duration) { b.mu.Lock(); b.retryDelay = delay; b.mu.Unlock() }

// Remove removes the segments in the batch from disk.
func (b *Batch) Remove() error {
	b.mu.Lock()
	b.removed = true
	b.mu.Unlock()

	return b.batcher.Remove(b)
}

// RemoveSegments removes a subset of the batch from disk. It is used when an
// oversized transfer is split and only part of the batch was delivered.
func (b *Batch) RemoveSegments(segments []wal.SegmentInfo) error {
	if len(segments) == 0 {
		return nil
	}
	return b.batcher.Remove(&Batch{Segments: segments, Database: b.Database, Table: b.Table, Prefix: b.Prefix, batcher: b.batcher})
}

func (b *Batch) Paths() []string {
	var paths []string
	for _, v := range b.Segments {
		paths = append(paths, v.Path)
	}
	return paths
}

func (b *Batch) IsReleased() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.released
}

func (b *Batch) IsRemoved() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.removed
}

type Batcher interface {
	service.Component
	BatchSegments() error
	UploadQueueSize() int
	TransferQueueSize() int
	SegmentsTotal() int64
	SegmentsSize() int64

	Release(batch *Batch)
	Remove(batch *Batch) error
	MaxSegmentAge() time.Duration
}

// Batcher manages WAL segments that are ready for upload to kusto or that need
// to be transferred to another node.
type batcher struct {
	uploadQueue   chan *Batch
	transferQueue chan *Batch

	realtimeUploadQueue   chan *Batch
	realtimeTransferQueue chan *Batch

	// realtime batches realtime segments as they close.  When nil, the periodic scan batches them.
	realtime *eventBatcher

	// queued batches queued segments as they close.  It is nil when the Segmenter does not publish segment events, in
	// which case the periodic scan batches them.
	queued *eventBatcher

	store storage.Store

	// pendingUploads is the number of batches ready for upload but not in the upload queue.
	pendingUploads atomic.Uint64
	// pendingTransfers is the number of batches ready for transfer but not in the transfer queue.
	pendingTransfer atomic.Uint64

	// transferDisabled is set to true when transfers are disabled.
	transferDisabled bool

	wg         sync.WaitGroup
	batchMu    sync.Mutex
	closeFn    context.CancelFunc
	ctx        context.Context
	storageDir string

	Partitioner      MetricPartitioner
	Segmenter        Segmenter
	health           PeerHealthReporter
	hostname         string
	maxTransferAge   time.Duration
	maxTransferSize  int64
	minUploadSize    int64
	maxSegmentAge    time.Duration
	maxBatchSegments int
	maxTableBatches  int

	tempSet []wal.SegmentInfo

	// segments counts the batches each segment is part of.
	segments *partmap.Map[int]
	// markMu makes reading a prefix's unbatched segments and marking them as batched atomic with removing a delivered
	// segment from the index and clearing a released batch's marks.  Otherwise a segment read just before its batch is
	// delivered and released looks unbatched, and is batched again.
	markMu sync.Mutex

	usage SegmentUsage

	// Metrics for observability
	segmentsCountMetric     prometheus.Gauge
	segmentsSizeBytesMetric prometheus.Gauge
	segmentsMaxAgeMetric    prometheus.Gauge
	sizeByPriorityMetric    *prometheus.GaugeVec
}

func NewBatcher(opts BatcherOpts) (Batcher, error) {
	minUploadSize := opts.MinUploadSize
	if minUploadSize == 0 {
		minUploadSize = 100 * 1024 * 1024 // This is the minimal "optimal" size for kusto uploads.
	}

	maxBatchSegments := DefaultMaxSegmentCount
	if opts.MaxBatchSegments > 0 {
		maxBatchSegments = opts.MaxBatchSegments
	}

	// Ensure metrics are provided
	if opts.SegmentsCountMetric == nil {
		return nil, fmt.Errorf("SegmentsCountMetric is required")
	}
	if opts.SegmentsSizeBytesMetric == nil {
		return nil, fmt.Errorf("SegmentsSizeBytesMetric is required")
	}
	if opts.SegmentsMaxAgeMetric == nil {
		return nil, fmt.Errorf("SegmentsMaxAgeMetric is required")
	}

	b := &batcher{
		storageDir:              opts.StorageDir,
		ctx:                     context.Background(),
		maxTransferAge:          opts.MaxTransferAge,
		maxTransferSize:         opts.MaxTransferSize,
		minUploadSize:           minUploadSize,    // This is the minimal "optimal" size for kusto uploads.
		maxBatchSegments:        maxBatchSegments, // The maximum number of segments to include in a merged batch
		maxTableBatches:         max(2, opts.MaxTableBatches),
		Partitioner:             opts.Partitioner,
		Segmenter:               opts.Segmenter,
		uploadQueue:             opts.UploadQueue,
		transferQueue:           opts.TransferQueue,
		realtimeUploadQueue:     opts.RealtimeUploadQueue,
		realtimeTransferQueue:   opts.RealtimeTransferQueue,
		health:                  opts.PeerHealthReporter,
		transferDisabled:        opts.TransfersDisabled,
		segments:                partmap.NewMap[int](64),
		usage:                   opts.Usage,
		segmentsCountMetric:     opts.SegmentsCountMetric,
		segmentsSizeBytesMetric: opts.SegmentsSizeBytesMetric,
		segmentsMaxAgeMetric:    opts.SegmentsMaxAgeMetric,
		sizeByPriorityMetric:    opts.SegmentsSizeByPriorityMetric,
	}

	if _, ok := opts.Segmenter.(segmentSubscriber); ok && opts.Realtime.enabled() {
		b.realtime = newRealtimeBatcher(b, opts.Realtime)
	}

	// Queued segments are batched as they close.  The periodic scan is only used for segmenters that do not publish
	// segment events.
	if _, ok := opts.Segmenter.(segmentSubscriber); ok {
		linger := opts.QueuedLinger
		if linger <= 0 {
			linger = DefaultQueuedLinger
		}
		b.queued = newQueuedBatcher(b, linger, opts.TransferLinger)
		if opts.MaxRetryDelay > 0 {
			b.queued.maxRetryDelay = opts.MaxRetryDelay
		}
	}

	return b, nil
}

func (b *batcher) Open(ctx context.Context) error {
	ctx, b.closeFn = context.WithCancel(ctx)
	b.ctx = ctx
	var err error
	b.hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	if b.realtime != nil {
		b.realtime.Open(ctx)
	}
	if b.queued != nil {
		b.queued.Open(ctx)
	}

	b.wg.Add(1)
	go b.watch(ctx)

	return nil
}

func (b *batcher) Close() error {
	b.closeFn()
	if b.realtime != nil {
		b.realtime.Close()
	}
	if b.queued != nil {
		b.queued.Close()
	}
	b.wg.Wait()
	return nil
}

func (b *batcher) TransferQueueSize() int {
	return len(b.transferQueue) + separateQueueLen(b.realtimeTransferQueue, b.transferQueue) + int(b.pendingTransfer.Load())
}

func (b *batcher) UploadQueueSize() int {
	return len(b.uploadQueue) + separateQueueLen(b.realtimeUploadQueue, b.uploadQueue) + int(b.pendingUploads.Load())
}

// separateQueueLen returns the length of q if it is set and distinct from fallback so it is not counted twice.
func separateQueueLen(q, fallback chan *Batch) int {
	if q == nil || q == fallback {
		return 0
	}
	return len(q)
}

// uploadQueueFor returns the upload queue for batches with priority p.
func (b *batcher) uploadQueueFor(p ingestpolicy.Priority) chan *Batch {
	if p == ingestpolicy.PriorityRealtime && b.realtimeUploadQueue != nil {
		return b.realtimeUploadQueue
	}
	return b.uploadQueue
}

// transferQueueFor returns the transfer queue for batches with priority p.
func (b *batcher) transferQueueFor(p ingestpolicy.Priority) chan *Batch {
	if p == ingestpolicy.PriorityRealtime && b.realtimeTransferQueue != nil {
		return b.realtimeTransferQueue
	}
	return b.transferQueue
}

func (b *batcher) SegmentsTotal() int64 {
	if b.usage != nil {
		return b.usage.SegmentCount()
	}
	return int64(b.Segmenter.TotalSegments())
}

func (b *batcher) SegmentsSize() int64 {
	if b.usage != nil {
		return b.usage.Size()
	}
	return b.Segmenter.TotalSize()
}

// IsBatched returns true if the segment at path is part of a batch that has not been released or removed yet.
func (b *batcher) IsBatched(path string) bool {
	n, _ := b.segments.Get(path)
	return n > 0
}

func (b *batcher) MaxSegmentAge() time.Duration {
	return b.Segmenter.OldestSegmentAge()
}

func (b *batcher) watch(ctx context.Context) {
	defer b.wg.Done()

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Segments are batched as they close so the periodic tick only updates metrics.
			if b.queued != nil {
				b.updateSegmentMetrics()
				b.updateQueueMetrics(0, 0)
				continue
			}
			if err := b.BatchSegments(); err != nil {
				logger.Errorf("Failed to batch segments: %v", err)
			}
		}
	}
}

func (b *batcher) BatchSegments() error {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	if b.realtime != nil {
		b.realtime.Flush(context.Background())
	}
	if b.queued != nil {
		b.queued.Flush(context.Background())
		return nil
	}

	owned, notOwned, err := b.processSegments()
	if err != nil {
		return fmt.Errorf("process segments: %w", err)
	}
	owned = boundedBatches(owned, b.maxTableBatches)
	notOwned = boundedBatches(notOwned, b.maxTableBatches)
	b.pendingUploads.Store(uint64(len(owned)))
	b.pendingTransfer.Store(uint64(len(notOwned)))

	b.updateQueueMetrics(len(owned), len(notOwned))

	// Send realtime batches first so they are not delayed behind queued batches when queues are full.
	for _, p := range []ingestpolicy.Priority{ingestpolicy.PriorityRealtime, ingestpolicy.PriorityQueued} {
		for _, v := range owned {
			if v.Priority == p {
				b.uploadQueueFor(p) <- v
			}
		}
		for _, v := range notOwned {
			if v.Priority == p {
				b.transferQueueFor(p) <- v
			}
		}
	}
	b.pendingUploads.Store(0)
	b.pendingTransfer.Store(0)

	return nil
}

// boundedBatches caps how many batches one destination contributes to a single
// dispatch round. Excess batches stay on disk and are rediscovered by the next
// scan, so a backlogged table cannot fill a shared queue ahead of every other
// destination.
func boundedBatches(batches []*Batch, limit int) []*Batch {
	if limit <= 0 {
		return batches
	}
	counts := make(map[wal.Destination]int)
	result := batches[:0]
	for _, batch := range batches {
		d := wal.Destination{Database: batch.Database, Table: batch.Table}
		if counts[d] >= limit {
			metrics.IngestorDeferredBatches.WithLabelValues(d.Database, d.Table).Inc()
			batch.Release()
			continue
		}
		counts[d]++
		result = append(result, batch)
	}
	return result
}

// updateQueueMetrics updates the queue size metrics with the batches waiting to be queued.
func (b *batcher) updateQueueMetrics(pendingUploads, pendingTransfers int) {
	metrics.IngestorQueueSize.WithLabelValues("upload").Set(float64(len(b.uploadQueue) + pendingUploads))
	metrics.IngestorQueueSize.WithLabelValues("transfer").Set(float64(len(b.transferQueue) + pendingTransfers))
}

// updateSegmentMetrics updates the WAL segment metrics.
func (b *batcher) updateSegmentMetrics() {
	b.segmentsSizeBytesMetric.Set(float64(b.SegmentsSize()))
	if sizer, ok := b.Segmenter.(prioritySizer); ok && b.sizeByPriorityMetric != nil {
		for p := ingestpolicy.Priority(0); int(p) < ingestpolicy.NumPriorities; p++ {
			b.sizeByPriorityMetric.WithLabelValues(p.String()).Set(float64(sizer.TotalSizeByPriority(p)))
		}
	}
	b.segmentsCountMetric.Set(float64(b.SegmentsTotal()))
	b.segmentsMaxAgeMetric.Set(b.MaxSegmentAge().Seconds())
}

// splitPrefix splits the unbatched segments of prefix into batches and marks the segments as batched.  Batches are
// split by max segment count, upload size, transfer size and transfer age.  The returned owned batches are uploaded by
// this node and notOwned batches are transferred to the peer that owns the prefix.
func (b *batcher) splitPrefix(prefix string, v []wal.SegmentInfo) (owned, notOwned []*Batch) {
	return b.splitPrefixN(prefix, v, 0)
}

// splitPrefixN is like splitPrefix, but when maxBatches is positive it makes at most that many batches and leaves the
// remaining segments unbatched, oldest first, so a backlog is not split into more batches than can be queued at once.
func (b *batcher) splitPrefixN(prefix string, v []wal.SegmentInfo, maxBatches int) (owned, notOwned []*Batch) {
	if len(v) == 0 {
		return nil, nil
	}

	sort.Slice(v, func(i, j int) bool {
		return v[i].Path < v[j].Path
	})

	var (
		batchSize int64
		batch     *Batch
		// local is set when the batch holds a segment past the transfer age, so it is uploaded here.
		local bool
	)

	db, table, _, _, err := wal.ParseFilename(v[0].Path)
	if err != nil {
		logger.Errorf("Failed to parse segment filename: %s", err)
		return nil, nil
	}

	// All segments of a prefix belong to the same table and have the same priority.
	priority := v[0].Priority
	newBatch := func() *Batch {
		return &Batch{
			Prefix:   prefix,
			Database: db,
			Table:    table,
			Priority: priority,
			batcher:  b,
		}
	}
	batch = newBatch()

	for _, si := range v {
		if maxBatches > 0 && len(batch.Segments) == 0 && len(owned)+len(notOwned) >= maxBatches {
			break
		}

		// If the file has been on disk for more than the transfer age, we're behind on uploading so upload it directly
		// ourselves vs transferring it to another node, rather than have a node that's behind on uploading.  Segments
		// past the transfer age are batched together, so a backlog is uploaded in batches as large as the limits
		// allow rather than one segment at a time.  A batch of them ends before a segment young enough to transfer.
		old := b.maxTransferAge > 0 && time.Since(si.CreatedAt) > b.maxTransferAge
		if local && !old && len(batch.Segments) > 0 {
			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
			if maxBatches > 0 && len(owned)+len(notOwned) >= maxBatches {
				break
			}
		}
		local = old

		batch.Segments = append(batch.Segments, si)
		batchSize += si.Size

		// Record that this segment is part of a batch.
		_ = b.segments.Mutate(si.Path, func(n int) (int, error) {
			return n + 1, nil
		})

		// Prevent trying to combine an unbounded number of segments at once even if they are very small.  This
		// can incur a lot of CPU time and slower transfers when there are hundreds of segments that can be combined.
		if len(batch.Segments) >= b.maxBatchSegments {
			if logger.IsDebug() {
				logger.Debugf("Batch %s is merging more than %d segments, uploading directly", si.Path, 25)
			}

			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
			local = false
			continue
		}

		// The batch is at the optimal size for uploading to kusto, upload directly and start a new batch.
		if b.minUploadSize > 0 && batchSize >= b.minUploadSize {
			if logger.IsDebug() {
				logger.Debugf("Batch %s is larger than %dMB (%d), uploading directly", si.Path, (b.minUploadSize)/1e6, batchSize)
			}

			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
			local = false
			continue
		}

		if b.maxTransferSize > 0 && batchSize >= b.maxTransferSize {
			if logger.IsDebug() {
				logger.Debugf("Batch %s is larger than %dMB (%d), uploading directly", si.Path, b.maxTransferSize/1e6, batchSize)
			}

			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
			local = false
			continue
		}
	}

	if len(batch.Segments) == 0 {
		return owned, notOwned
	}

	owner, _ := b.Partitioner.Owner([]byte(prefix))

	// If the peer has signaled that it's unhealthy, upload the segments directly.
	peerHealthy := b.health.IsPeerHealthy(owner)

	// Realtime batches are uploaded by the node that has them rather than transferred to a peer to avoid the added
	// latency.
	if local || owner == b.hostname || !peerHealthy || b.transferDisabled || priority == ingestpolicy.PriorityRealtime {
		owned = append(owned, batch)
	} else {
		notOwned = append(notOwned, batch)
	}

	return owned, notOwned
}

// processSegments returns the set of batches that are owned by the current instance and
// the set that are owned by peers and need to be transferred.  The owned slice may contain
// segments that are owned by other peers if they are already past the max age or max size
// thresholds.  In addition, the batches are ordered as oldest first to allow for prioritizing
// lagging segments over new ones.
func (b *batcher) processSegments() ([]*Batch, []*Batch, error) {
	b.updateSegmentMetrics()

	// Groups is b map of metrics name to b list of segments for that metric.
	groups := make(map[string][]wal.SegmentInfo)

	byAge := b.Segmenter.PrefixesByAge()

	// Order them by newest first so we process recent data first.
	slices.Reverse(byAge)

	// Add a small percentage of the oldest segments to the front of the list to ensure that
	// we're ticking away through the backlog.
	byAge = prioritizeOldest(byAge)

	// We need to find the segment that this node is responsible for uploading to kusto and ones that
	// need to be transferred to other nodes.
	var (
		owned, notOwned       []*Batch
		groupSize             int
		totalFiles, totalSize int64
	)

	b.maxSegmentAge = 0

	for _, prefix := range byAge {
		b.tempSet = b.Segmenter.Get(b.tempSet[:0], prefix)

		// Remove any segments that are already part of a batch so we don't try to double upload them
		b.tempSet = slices.DeleteFunc(b.tempSet, func(si wal.SegmentInfo) bool {
			n, _ := b.segments.Get(si.Path)
			return n > 0
		})

		// If all the segments are already part of a batch, skip this prefix.
		if len(b.tempSet) == 0 {
			continue
		}

		// Realtime segments are batched as they close by the realtime batcher.
		if b.realtime != nil && b.tempSet[0].Priority == ingestpolicy.PriorityRealtime {
			continue
		}

		groupSize = 0
		var oldestSegment time.Time
		for _, v := range b.tempSet {
			if oldestSegment.IsZero() || v.CreatedAt.Before(oldestSegment) {
				oldestSegment = v.CreatedAt
			}
			groupSize += int(v.Size)
			totalFiles++
		}

		totalSize += int64(groupSize)

		if v := time.Since(oldestSegment); v > b.maxSegmentAge {
			b.maxSegmentAge = v
		}
		groups[prefix] = append(groups[prefix], b.tempSet...)
	}

	// For each sample, sort the segments by name.  The last segment is the current segment.
	for _, prefix := range byAge {
		o, n := b.splitPrefix(prefix, groups[prefix])
		owned = append(owned, o...)
		notOwned = append(notOwned, n...)
	}
	return owned, notOwned, nil
}

func (b *batcher) Release(batch *Batch) {
	b.markMu.Lock()
	for _, si := range batch.Segments {
		// Remove the segment from the map if it's no longer part of b batch so we don't leak keys
		_, _ = b.segments.Delete(si.Path)
	}
	b.markMu.Unlock()

	// Segments of a batch that was not removed, such as after a failed upload or transfer, are not announced again
	// by the index so they are retried by the event batcher that batched them.
	if batch.IsRemoved() {
		return
	}
	batch.mu.Lock()
	retryAfter, queueFull := batch.retryDelay, batch.queueFull
	batch.mu.Unlock()
	r := b.queued
	if b.realtime != nil && batch.Priority == ingestpolicy.PriorityRealtime {
		r = b.realtime
	}
	if r == nil {
		return
	}
	if queueFull {
		r.queueFull(batch.Prefix)
	} else {
		r.released(batch.Prefix, retryAfter)
	}
}

func (b *batcher) Remove(batch *Batch) error {
	if b.queued != nil {
		b.queued.removed(batch.Prefix)
	}
	for _, si := range batch.Segments {
		err := os.Remove(si.Path)
		if err != nil && !os.IsNotExist(err) {
			logger.Errorf("Failed to remove segment %s: %s", si.Path, err)
			continue
		}
		b.markMu.Lock()
		b.Segmenter.Remove(si)
		b.markMu.Unlock()
	}
	return nil
}

func prioritizeOldest(a []string) []string {
	var b []string

	// Find the index that is roughly 10% from the end of the list
	idx := len(a) - int(math.Round(float64(len(a))*0.2))
	// Move last 20% of batches to the front of the list
	b = append(b, a[idx:]...)
	// Reverse the list so the oldest batches are first
	slices.Reverse(b)
	// Move first 80% of batches to the end of the list
	b = append(b, a[:idx]...)
	return b
}
