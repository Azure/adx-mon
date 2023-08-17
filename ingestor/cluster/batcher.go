package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/flake"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
)

type Segmenter interface {
	IsActiveSegment(path string) bool
}

type BatcherOpts struct {
	StorageDir     string
	MaxSegmentAge  time.Duration
	MaxSegmentSize int64

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

	Partitioner    MetricPartitioner
	Segmenter      Segmenter
	hostname       string
	maxSegmentAge  time.Duration
	maxSegmentSize int64
}

func NewBatcher(opts BatcherOpts) Batcher {
	return &batcher{
		storageDir:     opts.StorageDir,
		maxSegmentAge:  90 * time.Second,
		maxSegmentSize: 100 * 1024 * 1024, // This is the minimal "optimal" size for kusto uploads.
		Partitioner:    opts.Partitioner,
		Segmenter:      opts.Segmenter,
		uploadQueue:    opts.UploadQueue,
		transferQueue:  opts.TransferQueue,
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
	entries, err := os.ReadDir(a.storageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read storage dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	metrics.IngestorSegmentsTotal.Reset()

	// Groups is a map of metrics name to a list of segments for that metric.
	groups := make(map[string][]string)

	// We need to find the segment that this node is responsible for uploading to kusto and ones that
	// need to be transferred to other nodes.
	var (
		owned, notOwned [][]string
		lastMetric      string
		groupSize       int
	)
	for _, v := range entries {
		if v.IsDir() || !strings.HasSuffix(v.Name(), ".wal") {
			continue
		}

		fi, err := v.Info()
		if err != nil {
			logger.Warn("Failed to stat file: %s", filepath.Join(a.storageDir, v.Name()))
			continue
		}
		groupSize += int(fi.Size())

		parts := strings.Split(v.Name(), "_")
		if len(parts) != 2 { // Cpu_1234.wal
			logger.Warn("Invalid file name: %s", filepath.Join(a.storageDir, v.Name()))
			continue
		}

		epoch := parts[1][:len(parts[1])-4]
		createdAt, err := flake.ParseFlakeID(epoch)
		if err != nil {
			logger.Warn("Failed to parse flake id: %s: %s", epoch, err)
		} else {
			if lastMetric == "" || parts[0] != lastMetric {
				metrics.IngestorSegmentsMaxAge.WithLabelValues(lastMetric).Set(time.Since(createdAt).Seconds())
				metrics.IngestorSegmentsSizeBytes.WithLabelValues(lastMetric).Set(float64(groupSize))
				groupSize = 0
			}
		}
		lastMetric = parts[0]

		metrics.IngestorSegmentsTotal.WithLabelValues(parts[0]).Inc()

		if a.Segmenter.IsActiveSegment(filepath.Join(a.storageDir, v.Name())) {
			if logger.IsDebug() {
				logger.Debug("Skipping active segment: %s", filepath.Join(a.storageDir, v.Name()))
			}
			continue
		}

		groups[parts[0]] = append(groups[parts[0]], filepath.Join(a.storageDir, v.Name()))
	}

	// For each metric, sort the segments by name.  The last segment is the current segment.
	for k, v := range groups {
		sort.Strings(v)

		var (
			directUpload bool
			fileSum      int64
		)

		for _, path := range v {
			stat, err := os.Stat(path)
			if err != nil {
				logger.Warn("Failed to stat file: %s", path)
				continue
			}

			fileSum += stat.Size()

			// If any of the files are larger than 100MB, we'll just upload it directly since it's already pretty big.
			if stat.Size() >= a.maxSegmentSize {
				if logger.IsDebug() {
					logger.Debug("File %s is larger than 100MB (%d), uploading directly", path, stat.Size())
				}
				directUpload = true
				break
			}

			createdAt, err := segmentCreationTime(path)
			if err != nil {
				logger.Warn("failed to determine segment creation time: %s", err)
			}

			// If the file has been on disk for more than 30 seconds, we're behind on uploading so upload it directly
			// ourselves vs transferring it to another node.  This could result in suboptimal upload batches, but we'd
			// rather take that hit than have a node that's behind on uploading.
			if time.Since(createdAt) > a.maxSegmentAge {
				if logger.IsDebug() {
					logger.Debug("File %s is older than %s (%s) seconds, uploading directly", path, a.maxSegmentAge.String(), time.Since(createdAt).String())
				}
				directUpload = true
				break
			}
		}

		if logger.IsDebug() && fileSum >= a.maxSegmentSize {
			logger.Debug("Metric %s is larger than 100MB (%d), uploading directly", k, fileSum)
		}

		// If the file is already >= 100MB, we'll just upload it directly because it's already in the optimal size range.
		// Transferring it to another node would just add additional latency and Disk IO
		if directUpload || fileSum >= a.maxSegmentSize {
			owned = append(owned, append([]string{}, v...))
			continue
		}

		// TODO: Should break up groups into files that are less than 1GB.
		owner, _ := a.Partitioner.Owner([]byte(k))
		if owner == a.hostname {
			owned = append(owned, append([]string{}, v...))
		} else {
			notOwned = append(notOwned, append([]string{}, v...))
		}
	}

	// Sort the owned and not-owned batches by creation time so that we prioritize uploading the old segments first
	sort.Slice(owned, func(i, j int) bool {
		groupA := owned[i]
		groupB := owned[j]

		minA := minCreated(groupA)
		minB := minCreated(groupB)
		return minA.Before(minB)
	})

	sort.Slice(notOwned, func(i, j int) bool {
		groupA := notOwned[i]
		groupB := notOwned[j]

		minA := minCreated(groupA)
		minB := minCreated(groupB)
		return minA.Before(minB)
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
	parts := strings.Split(filepath.Base(filename), "_")
	if len(parts) != 2 { // Cpu_1234.wal
		return time.Time{}, fmt.Errorf("invalid file name: %s", filename)
	}

	epoch := parts[1][:len(parts[1])-4]
	createdAt, err := flake.ParseFlakeID(epoch)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid file name: %s: %w", filename, err)

	}
	return createdAt, nil
}
