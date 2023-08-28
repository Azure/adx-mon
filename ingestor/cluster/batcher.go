package cluster

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
)

type Segmenter interface {
	IsActiveSegment(path string) bool
}

type BatcherOpts struct {
	StorageDir      string
	MaxSegmentAge   time.Duration
	MaxTransferSize int64
	MaxTransferAge  time.Duration

	Partitioner MetricPartitioner
	Segmenter   Segmenter

	UploadQueue   chan []string
	TransferQueue chan []string
}

type Batcher interface {
	service.Component
	BatchSegments() error
}

// Batcher manages WAL segments that are ready for upload to kusto or that need
// to be transferred to another node.
type batcher struct {
	uploadQueue   chan []string
	transferQueue chan []string

	wg         sync.WaitGroup
	closeFn    context.CancelFunc
	storageDir string

	Partitioner     MetricPartitioner
	Segmenter       Segmenter
	hostname        string
	maxTransferAge  time.Duration
	maxTransferSize int64
}

func NewBatcher(opts BatcherOpts) Batcher {
	return &batcher{
		storageDir:      opts.StorageDir,
		maxTransferAge:  opts.MaxTransferAge,
		maxTransferSize: opts.MaxTransferSize, // This is the minimal "optimal" size for kusto uploads.
		Partitioner:     opts.Partitioner,
		Segmenter:       opts.Segmenter,
		uploadQueue:     opts.UploadQueue,
		transferQueue:   opts.TransferQueue,
	}
}

func (a *batcher) Open(ctx context.Context) error {
	ctx, a.closeFn = context.WithCancel(ctx)
	var err error
	a.hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	go a.watch(ctx)

	return nil
}

func (a *batcher) Close() error {
	a.closeFn()
	a.wg.Wait()
	return nil
}

func (a *batcher) watch(ctx context.Context) {
	a.wg.Add(1)
	defer a.wg.Done()

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.BatchSegments(); err != nil {
				logger.Error("Failed to batch segments: %v", err)
				metrics.IngestorWalErrors.WithLabelValues(metrics.BatchSegmentsError).Add(1)
			}
		}
	}
}

func (a *batcher) BatchSegments() error {
	owned, notOwned, err := a.processSegments()
	if err != nil {
		return fmt.Errorf("process segments: %w", err)
	}

	for _, v := range owned {
		a.uploadQueue <- v
	}

	for _, v := range notOwned {
		a.transferQueue <- v
	}

	return nil
}

// processSegments returns the set of batches that are owned by the current instance and
// the set that are owned by peers and need to be transferred.  The owned slice may contain
// segments that are owned by other peers if they are already past the max age or max size
// thresholds.  In addition, the batches are ordered as oldest first to allow for prioritizing
// lagging segments over new ones.
func (a *batcher) processSegments() ([][]string, [][]string, error) {
	entries, err := wal.ListDir(a.storageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read storage dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	metrics.IngestorSegmentsCount.Reset()

	// Groups is a map of metrics name to a list of segments for that metric.
	groups := make(map[string][]string)

	// We need to find the segment that this node is responsible for uploading to kusto and ones that
	// need to be transferred to other nodes.
	var (
		owned, notOwned [][]string
		lastSegmentKey  string
		groupSize       int
	)
	for _, v := range entries {
		fi, err := os.Stat(v.Path)
		if err != nil {
			logger.Warn("Failed to stat file: %s", v.Path)
			continue
		}
		groupSize += int(fi.Size())

		createdAt, err := flake.ParseFlakeID(v.Epoch)
		if err != nil {
			logger.Warn("Failed to parse flake id: %s: %s", v.Epoch, err)
		} else {
			if lastSegmentKey == "" || v.Key != lastSegmentKey {
				metrics.IngestorSegmentsMaxAge.WithLabelValues(lastSegmentKey).Set(time.Since(createdAt).Seconds())
				metrics.IngestorSegmentsSizeBytes.WithLabelValues(lastSegmentKey).Set(float64(groupSize))
				groupSize = 0
			}
		}
		lastSegmentKey = v.Key

		metrics.IngestorSegmentsCount.WithLabelValues(v.Key).Inc()

		if a.Segmenter.IsActiveSegment(v.Path) {
			if logger.IsDebug() {
				logger.Debug("Skipping active segment: %s", v.Path)
			}
			continue
		}

		groups[v.Key] = append(groups[v.Key], v.Path)
	}

	// For each sample, sort the segments by name.  The last segment is the current segment.
	for k, v := range groups {
		sort.Strings(v)

		var (
			batchSize int64
			batch     []string
			maxAge    time.Duration
		)
		owner, _ := a.Partitioner.Owner([]byte(k))
		isOwned := strconv.FormatBool(owner == a.hostname)
		for _, path := range v {
			stat, err := os.Stat(path)
			if err != nil {
				logger.Warn("Failed to stat file: %s", path)
				metrics.IngestorWalErrors.WithLabelValues(metrics.StatFileError).Inc()
				continue
			}

			batch = append(batch, path)

			createdAt, err := segmentCreationTime(path)
			if err != nil {
				logger.Warn("failed to determine segment creation time: %s", err)
				metrics.IngestorWalErrors.WithLabelValues(metrics.ParseFilenameError).Inc()
			}

			age := time.Since(createdAt)
			if age > maxAge {
				maxAge = age
			}

			batchSize += stat.Size()
			if batchSize >= a.maxTransferSize {
				if logger.IsDebug() {
					logger.Debug("Batch %s is larger than %dMB (%d), uploading directly", a.maxTransferSize/1e6, path, stat.Size())
				}
				owned = append(owned, batch)
				batch = nil
				batchSize = 0

				metrics.FileSizeUploadBytes.WithLabelValues(k).Observe(float64(batchSize))
				metrics.FileUploadAge.WithLabelValues(k).Observe(maxAge.Seconds())
				metrics.FileUploadTotal.WithLabelValues(k, "size", isOwned).Inc()

				continue
			}

			// If the file has been on disk for more than 30 seconds, we're behind on uploading so upload it directly
			// ourselves vs transferring it to another node.  This could result in suboptimal upload batches, but we'd
			// rather take that hit than have a node that's behind on uploading.
			if time.Since(createdAt) > a.maxTransferAge {
				if logger.IsDebug() {
					logger.Debug("File %s is older than %s (%s) seconds, uploading directly", path, a.maxTransferAge.String(), time.Since(createdAt).String())
				}
				owned = append(owned, batch)
				batch = nil

				metrics.FileSizeUploadBytes.WithLabelValues(k).Observe(float64(batchSize))
				metrics.FileUploadAge.WithLabelValues(k).Observe(maxAge.Seconds())
				metrics.FileUploadTotal.WithLabelValues(k, "age", isOwned).Inc()
				continue
			}
		}

		if len(batch) == 0 {
			continue
		}

		if owner == a.hostname {
			owned = append(owned, batch)
		} else {
			notOwned = append(notOwned, batch)
		}
	}

	// Sort the owned and not-owned batches by creation time so that we prioritize uploading the old segments first
	sort.Slice(owned, func(i, j int) bool {
		groupA := owned[i]
		groupB := owned[j]

		minA := minCreated(groupA)
		minB := minCreated(groupB)
		return minB.Before(minA)
	})

	sort.Slice(notOwned, func(i, j int) bool {
		groupA := notOwned[i]
		groupB := notOwned[j]

		minA := minCreated(groupA)
		minB := minCreated(groupB)
		return minB.Before(minA)
	})

	return owned, notOwned, nil
}

func minCreated(batch []string) time.Time {
	var minTime time.Time
	for _, v := range batch {
		createdAt, err := segmentCreationTime(v)
		if err != nil {
			logger.Warn("Invalid file name: %s: %s", v, err)
			continue
		}

		if minTime.IsZero() || createdAt.Before(minTime) {
			minTime = createdAt
		}
	}
	return minTime
}

func segmentCreationTime(filename string) (time.Time, error) {
	_, _, epoch, err := wal.ParseFilename(filename)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid file name: %s: %w", filename, err)
	}

	createdAt, err := flake.ParseFlakeID(epoch)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid file name: %s: %w", filename, err)

	}
	return createdAt, nil
}
