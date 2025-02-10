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
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/partmap"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/storage"
)

type Segmenter interface {
	Get(infos []wal.SegmentInfo, prefix string) []wal.SegmentInfo
	PrefixesByAge() []string
	Remove(si wal.SegmentInfo)
}

type BatcherOpts struct {
	StorageDir      string
	MinUploadSize   int64
	MaxSegmentAge   time.Duration
	MaxTransferSize int64
	MaxTransferAge  time.Duration

	Partitioner MetricPartitioner
	Segmenter   Segmenter

	UploadQueue        chan *Batch
	TransferQueue      chan *Batch
	PeerHealthReporter PeerHealthReporter

	TransfersDisabled bool
}

type Batch struct {
	Segments []wal.SegmentInfo
	Database string
	Table    string
	Prefix   string

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
	store         storage.Store

	// pendingUploads is the number of batches ready for upload but not in the upload queue.
	pendingUploads uint64
	// pendingTransfers is the number of batches ready for transfer but not in the transfer queue.
	pendingTransfer uint64

	// segmentsTotal is the total number of segments on disk.
	segmentsTotal int64

	// segmentsSize is the total size of segments on disk.
	segementsSize int64

	// transferDisabled is set to true when transfers are disabled.
	transferDisabled bool

	wg         sync.WaitGroup
	closeFn    context.CancelFunc
	storageDir string

	Partitioner     MetricPartitioner
	Segmenter       Segmenter
	health          PeerHealthReporter
	hostname        string
	maxTransferAge  time.Duration
	maxTransferSize int64
	minUploadSize   int64
	maxSegmentAge   time.Duration

	tempSet []wal.SegmentInfo

	segments *partmap.Map[int]
}

func NewBatcher(opts BatcherOpts) Batcher {
	minUploadSize := opts.MinUploadSize
	if minUploadSize == 0 {
		minUploadSize = 100 * 1024 * 1024 // This is the minimal "optimal" size for kusto uploads.
	}
	return &batcher{
		storageDir:       opts.StorageDir,
		maxTransferAge:   opts.MaxTransferAge,
		maxTransferSize:  opts.MaxTransferSize,
		minUploadSize:    minUploadSize, // This is the minimal "optimal" size for kusto uploads.
		Partitioner:      opts.Partitioner,
		Segmenter:        opts.Segmenter,
		uploadQueue:      opts.UploadQueue,
		transferQueue:    opts.TransferQueue,
		health:           opts.PeerHealthReporter,
		transferDisabled: opts.TransfersDisabled,
		segments:         partmap.NewMap[int](64),
	}
}

func (b *batcher) Open(ctx context.Context) error {
	ctx, b.closeFn = context.WithCancel(ctx)
	var err error
	b.hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	go b.watch(ctx)

	return nil
}

func (b *batcher) Close() error {
	b.closeFn()
	b.wg.Wait()
	return nil
}

func (b *batcher) TransferQueueSize() int {
	return len(b.transferQueue) + int(atomic.LoadUint64(&b.pendingTransfer))
}

func (b *batcher) UploadQueueSize() int {
	return len(b.uploadQueue) + int(atomic.LoadUint64(&b.pendingUploads))
}

func (b *batcher) SegmentsTotal() int64 {
	return atomic.LoadInt64(&b.segmentsTotal)
}

func (b *batcher) SegmentsSize() int64 {
	return atomic.LoadInt64(&b.segementsSize)
}

func (b *batcher) MaxSegmentAge() time.Duration {
	return b.maxSegmentAge
}

func (b *batcher) watch(ctx context.Context) {
	b.wg.Add(1)
	defer b.wg.Done()

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := b.BatchSegments(); err != nil {
				logger.Errorf("Failed to batch segments: %v", err)
			}
		}
	}
}

func (b *batcher) BatchSegments() error {
	owned, notOwned, err := b.processSegments()
	if err != nil {
		return fmt.Errorf("process segments: %w", err)
	}
	atomic.StoreUint64(&b.pendingUploads, uint64(len(owned)))
	atomic.StoreUint64(&b.pendingTransfer, uint64(len(notOwned)))

	metrics.IngestorQueueSize.WithLabelValues("upload").Set(float64(len(b.uploadQueue) + len(owned)))
	metrics.IngestorQueueSize.WithLabelValues("transfer").Set(float64(len(b.transferQueue) + len(notOwned)))

	for _, v := range owned {
		b.uploadQueue <- v
	}

	for _, v := range notOwned {
		b.transferQueue <- v
	}

	return nil
}

// processSegments returns the set of batches that are owned by the current instance and
// the set that are owned by peers and need to be transferred.  The owned slice may contain
// segments that are owned by other peers if they are already past the max age or max size
// thresholds.  In addition, the batches are ordered as oldest first to allow for prioritizing
// lagging segments over new ones.
func (b *batcher) processSegments() ([]*Batch, []*Batch, error) {
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

		metrics.IngestorSegmentsMaxAge.WithLabelValues(prefix).Set(time.Since(oldestSegment).Seconds())
		metrics.IngestorSegmentsSizeBytes.WithLabelValues(prefix).Set(float64(groupSize))
		metrics.IngestorSegmentsTotal.WithLabelValues(prefix).Set(float64(len(b.tempSet)))
		b.maxSegmentAge = time.Since(oldestSegment)
		groups[prefix] = append(groups[prefix], b.tempSet...)
	}

	// For each sample, sort the segments by name.  The last segment is the current segment.
	for _, prefix := range byAge {
		v := groups[prefix]

		if len(v) == 0 {
			continue
		}

		sort.Slice(v, func(i, j int) bool {
			return v[i].Path < v[j].Path
		})

		var (
			batchSize    int64
			batch        *Batch
			directUpload bool
		)

		db, table, _, _, err := wal.ParseFilename(v[0].Path)
		if err != nil {
			logger.Errorf("Failed to parse segment filename: %s", err)
			continue
		}

		batch = &Batch{
			Prefix:   prefix,
			Database: db,
			Table:    table,
			batcher:  b,
		}

		for _, si := range v {
			batch.Segments = append(batch.Segments, si)
			batchSize += si.Size

			// Record that this segment is part of a batch.
			_ = b.segments.Mutate(si.Path, func(n int) (int, error) {
				return n + 1, nil
			})

			// The batch is at the optimal size for uploading to kusto, upload directly and start b new batch.
			if b.minUploadSize > 0 && batchSize >= b.minUploadSize {
				if logger.IsDebug() {
					logger.Debugf("Batch %s is larger than %dMB (%d), uploading directly", si.Path, (b.minUploadSize)/1e6, batchSize)
				}

				owned = append(owned, batch)
				batch = &Batch{
					Prefix:   prefix,
					Database: db,
					Table:    table,
					batcher:  b,
				}
				batchSize = 0
				directUpload = false
				continue
			}

			if b.maxTransferSize > 0 && batchSize >= b.maxTransferSize {
				if logger.IsDebug() {
					logger.Debugf("Batch %s is larger than %dMB (%d), uploading directly", si.Path, b.maxTransferSize/1e6, batchSize)
				}
				directUpload = true
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
				directUpload = true
			}
		}

		if len(batch.Segments) == 0 {
			continue
		}

		if directUpload {
			owned = append(owned, batch)
			batch = nil
			batchSize = 0
			continue
		}

		owner, _ := b.Partitioner.Owner([]byte(prefix))

		// If the peer has signaled that it's unhealthy, upload the segments directly.
		peerHealthy := b.health.IsPeerHealthy(owner)

		if owner == b.hostname || !peerHealthy || b.transferDisabled {
			owned = append(owned, batch)
		} else {
			notOwned = append(notOwned, batch)
		}
	}

	atomic.StoreInt64(&b.segmentsTotal, totalFiles)
	atomic.StoreInt64(&b.segementsSize, totalSize)

	return owned, notOwned, nil
}

func (b *batcher) Release(batch *Batch) {
	for _, si := range batch.Segments {
		// Remove the segment from the map if it's no longer part of b batch so we don't leak keys
		_, _ = b.segments.Delete(si.Path)
	}
}

func (b *batcher) Remove(batch *Batch) error {
	for _, si := range batch.Segments {
		err := os.Remove(si.Path)
		if err != nil && !os.IsNotExist(err) {
			logger.Errorf("Failed to remove segment %s: %s", si.Path, err)
			continue
		}
		b.Segmenter.Remove(si)
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
