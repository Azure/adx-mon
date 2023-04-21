package cluster

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Segmenter interface {
	IsActiveSegment(path string) bool
}

type ArchiverOpts struct {
	StorageDir  string
	Partitioner MetricPartitioner
	Segmenter   Segmenter

	UploadQueue   chan []string
	TransferQueue chan []string
}

type Archiver interface {
	service.Component
}

// Archiver manages WAL segments that are ready for upload to kusto or that need
// to be transferred to another node.
type archiver struct {
	uploadQueue   chan []string
	transferQueue chan []string

	wg         sync.WaitGroup
	closeFn    context.CancelFunc
	storageDir string

	Partitioner MetricPartitioner
	Segmenter   Segmenter
	hostname    string
}

func NewArchiver(opts ArchiverOpts) Archiver {
	return &archiver{
		storageDir:    opts.StorageDir,
		Partitioner:   opts.Partitioner,
		Segmenter:     opts.Segmenter,
		uploadQueue:   opts.UploadQueue,
		transferQueue: opts.TransferQueue,
	}
}

func (a *archiver) Open(ctx context.Context) error {
	ctx, a.closeFn = context.WithCancel(ctx)
	var err error
	a.hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	go a.watch(ctx)

	return nil
}

func (a *archiver) Close() error {
	a.closeFn()
	a.wg.Wait()
	return nil
}

func (a *archiver) watch(ctx context.Context) {
	a.wg.Add(1)
	defer a.wg.Done()

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			owned, notOwned, err := a.processSegments()
			if err != nil {
				logger.Error("Failed to process segments: %v", err)
				continue
			}

			// TODO: This might be removable now that uploader tracks files currently being uploaded.
			if len(a.uploadQueue) > 0 {
				logger.Info("uploadReader queue not empty (%d), skipping", len(a.uploadQueue))
			} else {
				for _, v := range owned {
					a.uploadQueue <- v
				}
			}

			if len(a.transferQueue) > 0 {
				logger.Info("Transfer queue not empty (%d), skipping", len(a.transferQueue))
			} else {
				for _, v := range notOwned {
					a.transferQueue <- v
				}
			}
		}
	}
}

func (a *archiver) processSegments() ([][]string, [][]string, error) {
	entries, err := os.ReadDir(a.storageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read storage dir: %w", err)
	}

	// Groups is a map of metrics name to a list of segments for that metric.
	groups := make(map[string][]string)

	// We need to find the segment that this node is responsible for uploading to kusto and ones that
	// need to be transferred to other nodes.
	var owned, notOwned [][]string
	for _, v := range entries {
		if v.IsDir() || !strings.HasSuffix(v.Name(), ".csv") {
			continue
		}

		if a.Segmenter.IsActiveSegment(filepath.Join(a.storageDir, v.Name())) {
			continue
		}

		parts := strings.Split(v.Name(), "_")
		if len(parts) != 2 { // Cpu_1234.csv
			logger.Warn("Invalid file name: %s", filepath.Join(a.storageDir, v.Name()))
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
			if stat.Size() >= 100*1024*1024 {
				directUpload = true
				break
			}

			// If the file has been on disk for more than 30 seconds, we're behind on uploading so upload it directly
			// ourselves vs transferring it to another node.  This could result in suboptimal upload batches, but we'd
			// rather take that hit than have a node that's behind on uploading.
			if time.Since(stat.ModTime()) > 30*time.Second {
				directUpload = true
				break
			}
		}

		// If the file is already >= 100MB, we'll just upload it directly because it's already in the optimal size range.
		// Transferring it to another node would just add additional latency and Disk IO
		if directUpload || fileSum >= 100*1024*1024 {
			owned = append(owned, append([]string{}, v...))
			continue
		}

		// TODO: Should order these by age and break up groups into files that are less than 1GB.
		owner, _ := a.Partitioner.Owner([]byte(k))
		if owner == a.hostname {
			owned = append(owned, append([]string{}, v...))
		} else {
			notOwned = append(notOwned, append([]string{}, v...))
		}
	}

	return owned, notOwned, nil
}
