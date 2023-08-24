package adx

import (
	"context"
	"io"
	"os"
	"sync"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

type Coordinator interface {
	UploadQueue() chan []string
	Open(ctx context.Context) error
	Close() error
}

type SegmentReader func(path string) (io.ReadCloser, error)

type coordinator struct {
	uploaders map[string]Uploader
	queue     chan []string
	cancel    context.CancelFunc
	reader    SegmentReader

	mu          sync.Mutex
	activeFiles map[string]struct{}
}

func NewCoordinator(uploaders []Uploader, fn SegmentReader, concurrentUploads int) *coordinator {
	c := &coordinator{
		uploaders:   make(map[string]Uploader),
		queue:       make(chan []string, concurrentUploads),
		activeFiles: make(map[string]struct{}),
		reader:      fn,
	}

	for _, uploader := range uploaders {
		c.uploaders[uploader.Database()] = uploader
	}

	return c
}

func (c *coordinator) Open(ctx context.Context) error {
	cctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	for i := 0; i < cap(c.queue); i++ {
		go c.handler(cctx)
	}
	return nil
}

func (c *coordinator) Close() error {
	c.cancel()
	for _, uploader := range c.uploaders {
		go uploader.Close()
	}
	return nil
}

func (c *coordinator) UploadQueue() chan []string {
	return c.queue
}

type streams struct {
	readers map[string][]io.Reader
	closers map[string][]io.Closer
	paths   map[string][]string
}

func (c *coordinator) handler(ctx context.Context) {

	for {
		select {
		case <-ctx.Done():
			return
		case paths := <-c.queue:
			c.handlePaths(ctx, paths)
		}
	}
}

func (c *coordinator) handlePaths(ctx context.Context, paths []string) {
	var (
		// groups are a map of database to tables of readers and closers.
		groups          = make(map[string]streams)
		ok              bool
		database, table string
	)

	// Group paths based on database and table.
	for _, path := range paths {

		// Ignore any files that are already being uploaded.
		c.mu.Lock()
		if _, ok = c.activeFiles[path]; ok {
			c.mu.Unlock()
			continue
		}
		c.mu.Unlock()

		// TODO: wal.NewSegmentReader(path)
		f, err := c.reader(path)

		if os.IsNotExist(err) {
			// batches are not disjoint, so the same segments could be included in multiple batches.
			continue
		} else if err != nil {
			logger.Error("Failed to open file: %s", err.Error())
			continue
		}

		database, table, _, err = wal.ParseFilename(path)
		if err != nil {
			logger.Error("Failed to parse file: %s", err.Error())
			f.Close()
			continue
		}
		if _, ok = c.uploaders[database]; !ok {
			logger.Error("No uploader for database: %s", database)
			f.Close()
			continue
		}

		c.mu.Lock()
		if _, ok = c.activeFiles[path]; ok {
			c.mu.Unlock()
			f.Close()
			continue
		}
		c.activeFiles[path] = struct{}{}
		c.mu.Unlock()

		_, ok = groups[database]
		if !ok {
			groups[database] = streams{
				readers: make(map[string][]io.Reader),
				closers: make(map[string][]io.Closer),
				paths:   make(map[string][]string),
			}
		}
		groups[database].readers[table] = append(groups[database].readers[table], f)
		groups[database].closers[table] = append(groups[database].closers[table], f)
		groups[database].paths[table] = append(groups[database].paths[table], path)
	}

	// Now upload each group.
	for database, group := range groups {
		for table, readers := range group.readers {
			if err := c.uploaders[database].Upload(ctx, database, table, io.MultiReader(readers...)); err != nil {
				logger.Error("Failed to upload file: %s", err.Error())
			}
			for _, closer := range group.closers[table] {
				closer.Close()
			}

			c.mu.Lock()
			for _, path := range group.paths[table] {
				delete(c.activeFiles, path)
			}
			c.mu.Unlock()
		}
	}
}
