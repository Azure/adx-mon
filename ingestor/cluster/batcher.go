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

type BatcherOpts struct {
	StorageDir       string
	MinUploadSize    int64
	MaxSegmentAge    time.Duration
	MaxTransferSize  int64
	MaxTransferAge   time.Duration
	MaxBatchSegments int

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

	// Mode selects how queued segments are batched.  Defaults to BatcherModeScan.
	Mode BatcherMode

	// QueuedLinger is the maximum time a closed queued segment waits to be batched with others in BatcherModeEvent.
	// Defaults to DefaultQueuedLinger.
	QueuedLinger time.Duration

	TransfersDisabled bool

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

	released bool
	removed  bool
	mu       sync.Mutex
}

// Release releases the segments in the batch so they can be processed again.
func (b *Batch) Release() {
	b.batcher.Release(b)

	b.mu.Lock()
	b.released = true
	b.mu.Unlock()
}

// Remove removes the segments in the batch from disk.
func (b *Batch) Remove() error {
	b.mu.Lock()
	b.removed = true
	b.mu.Unlock()

	return b.batcher.Remove(b)
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

	// queued batches queued segments as they close in BatcherModeEvent.  When nil, the periodic scan batches them.
	queued *eventBatcher

	store storage.Store

	// pendingUploads is the number of batches ready for upload but not in the upload queue.
	pendingUploads uint64
	// pendingTransfers is the number of batches ready for transfer but not in the transfer queue.
	pendingTransfer uint64

	// transferDisabled is set to true when transfers are disabled.
	transferDisabled bool

	wg         sync.WaitGroup
	closeFn    context.CancelFunc
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

	tempSet []wal.SegmentInfo

	segments *partmap.Map[int]

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
		maxTransferAge:          opts.MaxTransferAge,
		maxTransferSize:         opts.MaxTransferSize,
		minUploadSize:           minUploadSize,    // This is the minimal "optimal" size for kusto uploads.
		maxBatchSegments:        maxBatchSegments, // The maximum number of segments to include in a merged batch
		Partitioner:             opts.Partitioner,
		Segmenter:               opts.Segmenter,
		uploadQueue:             opts.UploadQueue,
		transferQueue:           opts.TransferQueue,
		realtimeUploadQueue:     opts.RealtimeUploadQueue,
		realtimeTransferQueue:   opts.RealtimeTransferQueue,
		health:                  opts.PeerHealthReporter,
		transferDisabled:        opts.TransfersDisabled,
		segments:                partmap.NewMap[int](64),
		segmentsCountMetric:     opts.SegmentsCountMetric,
		segmentsSizeBytesMetric: opts.SegmentsSizeBytesMetric,
		segmentsMaxAgeMetric:    opts.SegmentsMaxAgeMetric,
		sizeByPriorityMetric:    opts.SegmentsSizeByPriorityMetric,
	}

	if _, ok := opts.Segmenter.(segmentSubscriber); ok && opts.Realtime.enabled() {
		b.realtime = newRealtimeBatcher(b, opts.Realtime)
	}

	switch opts.Mode {
	case "", BatcherModeScan:
	case BatcherModeEvent:
		if _, ok := opts.Segmenter.(segmentSubscriber); !ok {
			return nil, fmt.Errorf("batcher mode %q requires a segmenter that publishes segment events", opts.Mode)
		}
		linger := opts.QueuedLinger
		if linger <= 0 {
			linger = DefaultQueuedLinger
		}
		b.queued = newQueuedBatcher(b, linger)
	default:
		return nil, fmt.Errorf("invalid batcher mode %q", opts.Mode)
	}

	return b, nil
}

func (b *batcher) Open(ctx context.Context) error {
	ctx, b.closeFn = context.WithCancel(ctx)
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
	return len(b.transferQueue) + separateQueueLen(b.realtimeTransferQueue, b.transferQueue) + int(atomic.LoadUint64(&b.pendingTransfer))
}

func (b *batcher) UploadQueueSize() int {
	return len(b.uploadQueue) + separateQueueLen(b.realtimeUploadQueue, b.uploadQueue) + int(atomic.LoadUint64(&b.pendingUploads))
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
	return int64(b.Segmenter.TotalSegments())
}

func (b *batcher) SegmentsSize() int64 {
	return b.Segmenter.TotalSize()
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
			// In event mode, segments are batched as they close so the periodic tick only updates metrics.
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
	atomic.StoreUint64(&b.pendingUploads, uint64(len(owned)))
	atomic.StoreUint64(&b.pendingTransfer, uint64(len(notOwned)))

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

	return nil
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
	if len(v) == 0 {
		return nil, nil
	}

	sort.Slice(v, func(i, j int) bool {
		return v[i].Path < v[j].Path
	})

	var (
		batchSize int64
		batch     *Batch
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
			continue
		}

		if b.maxTransferSize > 0 && batchSize >= b.maxTransferSize {
			if logger.IsDebug() {
				logger.Debugf("Batch %s is larger than %dMB (%d), uploading directly", si.Path, b.maxTransferSize/1e6, batchSize)
			}

			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
			continue
		}

		createdAt := si.CreatedAt

		// If the file has been on disk for more than 30 seconds, we're behind on uploading so upload it directly
		// ourselves vs transferring it to another node.  This could result in suboptimal upload batches, but we'd
		// rather take that hit than have b node that's behind on uploading.
		if b.maxTransferAge > 0 && time.Since(createdAt) > b.maxTransferAge {
			if logger.IsDebug() {
				logger.Debugf("File %s is older than %s (%s) seconds, uploading directly", si.Path, b.maxTransferAge.String(), time.Since(createdAt).String())
			}
			owned = append(owned, batch)
			batch = newBatch()
			batchSize = 0
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
	if owner == b.hostname || !peerHealthy || b.transferDisabled || priority == ingestpolicy.PriorityRealtime {
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
	for _, si := range batch.Segments {
		// Remove the segment from the map if it's no longer part of b batch so we don't leak keys
		_, _ = b.segments.Delete(si.Path)
	}

	// Segments of a realtime batch that was not removed, such as after a failed upload, are not announced again by
	// the index so they are retried by the realtime batcher.
	if b.realtime != nil && batch.Priority == ingestpolicy.PriorityRealtime && !batch.IsRemoved() {
		b.realtime.released(batch.Prefix)
	}
}

func (b *batcher) Remove(batch *Batch) error {
	for _, si := range batch.Segments {
		b.Segmenter.Remove(si)
		err := os.Remove(si.Path)
		if err != nil && !os.IsNotExist(err) {
			logger.Errorf("Failed to remove segment %s: %s", si.Path, err)
			continue
		}
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
