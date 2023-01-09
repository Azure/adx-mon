package storage

import (
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/prompb"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type WAL struct {
	opts       WALOpts
	compressor *Compressor

	path       string
	schemaPath string

	closedSegments chan Segment

	closing  chan struct{}
	mu       sync.RWMutex
	segments []Segment
}

type WALOpts struct {
	ClosedSegmentCh chan Segment

	StorageDir string

	// WAL segment prefix
	Prefix string

	// SegmentMaxSize is the max size of a segment file in bytes before it will be rotated and compressed.
	SegmentMaxSize int64

	// SegmentMaxAge is the max age of a segment file before it will be rotated and compressed.
	SegmentMaxAge time.Duration
}

func NewWAL(opts WALOpts) (*WAL, error) {
	if opts.StorageDir == "" {
		return nil, fmt.Errorf("wal storage dir not defined")
	}

	return &WAL{
		opts:           opts,
		compressor:     &Compressor{},
		closedSegments: make(chan Segment, 1),
		closing:        make(chan struct{}),
		segments:       []Segment{},
	}, nil
}

func (w *WAL) Open() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	prefix := fmt.Sprintf("%s_", w.opts.Prefix)
	if err := filepath.WalkDir(w.opts.StorageDir, func(path string, d os.DirEntry, err error) error {
		if d.IsDir() || filepath.Ext(path) != ".csv" {
			return nil
		}

		fileName := filepath.Base(path)

		if !strings.HasPrefix(fileName, prefix) {
			return nil
		}

		seg, err := OpenSegment(path)
		if err != nil {
			return err
		}

		w.segments = append(w.segments, seg)

		return nil
	}); err != nil {
		return err
	}

	sort.Slice(w.segments, func(j, k int) bool {
		return w.segments[j].Epoch() < w.segments[k].Epoch()
	})

	go w.rotate()
	go w.compress()

	return nil
}

func (w *WAL) Close() error {
	close(w.closing)

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, seg := range w.segments {
		if err := seg.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (w *WAL) Write(ts prompb.TimeSeries) error {
	var seg Segment

	w.mu.Lock()
	defer w.mu.Unlock()

	segs := w.segments

	if len(segs) == 0 {
		var err error
		seg, err := NewSegment(w.opts.StorageDir, w.opts.Prefix)
		if err != nil {
			return err
		}
		w.segments = []Segment{seg}
	}
	seg = w.segments[len(w.segments)-1]

	sz, err := seg.Size()
	if err != nil {
		return err
	}
	// Rotate the segment once it's past the max segment size
	if sz >= 0 &&
		(w.opts.SegmentMaxSize != 0 && sz >= w.opts.SegmentMaxSize) ||
		(w.opts.SegmentMaxAge.Seconds() != 0 && time.Since(seg.CreatedAt()) > w.opts.SegmentMaxAge) {

		w.closedSegments <- seg
		w.segments = w.segments[1:len(w.segments)]

		var err error
		seg, err = NewSegment(w.opts.StorageDir, w.opts.Prefix)
		if err != nil {
			return err
		}
		w.segments = append(w.segments, seg)
	}

	ts.Labels = ts.Labels[1:]

	return seg.Write(ts)
}

func (w *WAL) Size() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.segments)
}

func (w *WAL) Segment() Segment {
	w.mu.RLock()
	defer w.mu.RUnlock()
	segs := w.segments
	if len(segs) == 0 {
		return nil
	}
	return segs[0]
}

func (w *WAL) ClosedSegments() chan Segment {
	return w.closedSegments
}

func (w *WAL) rotate() {

	go func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		for _, seg := range w.segments {

			sz, err := seg.Size()
			if err != nil {
				logger.Error("Failed segment size: %s", err.Error())
				continue
			}
			// Rotate the segment once it's past the max segment size
			if sz >= 0 &&
				(w.opts.SegmentMaxSize != 0 && sz >= w.opts.SegmentMaxSize) ||
				(w.opts.SegmentMaxAge.Seconds() != 0 && time.Since(seg.CreatedAt()) > w.opts.SegmentMaxAge) {

				if err := seg.Close(); err != nil {
					logger.Warn("Failed to close segment: %s", err.Error())
					continue
				}

				select {
				case w.closedSegments <- seg:
				default:
					logger.Error("Failed to notify closed segment: channel full")
					continue
				}
				w.segments = w.segments[1:len(w.segments)]
			}
		}

	}()

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-w.closing:
			return
		case <-t.C:
			w.mu.Lock()

			for _, seg := range w.segments {

				sz, err := seg.Size()
				if err != nil {
					logger.Error("Failed segment size: %s", err.Error())
					continue
				}

				// Rotate the segment once it's past the max segment size
				if sz >= 0 &&
					(w.opts.SegmentMaxSize != 0 && sz >= w.opts.SegmentMaxSize) ||
					(w.opts.SegmentMaxAge.Seconds() != 0 && time.Since(seg.CreatedAt()) > w.opts.SegmentMaxAge) {

					if err := seg.Close(); err != nil {
						logger.Error("Failed segement close: %s", err.Error())
						continue
					}

					select {
					case w.closedSegments <- seg:
					default:
						logger.Error("Failed to notify closed segment: channel full")
						continue
					}
					w.segments = w.segments[1:len(w.segments)]
				}
			}
			w.mu.Unlock()
		}

	}
}

func (w *WAL) compress() {
	for {
		select {
		case <-w.closing:
			return
		case seg := <-w.closedSegments:
			if err := seg.Close(); err != nil {
				logger.Error("Failed segment close: %s", err.Error())
				continue
			}

			path, err := w.compressor.Compress(seg)
			if err != nil {
				logger.Error("Failed segment compress: %s", err.Error())
				continue
			}
			_ = os.RemoveAll(seg.Path())

			logger.Info("Archived %s", path)
		}
	}
}
