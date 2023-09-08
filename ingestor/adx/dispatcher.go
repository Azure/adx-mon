package adx

import (
	"context"
	"os"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
)

type dispatcher struct {
	uploaders map[string]Uploader
	queue     chan *cluster.Batch
	cancel    context.CancelFunc

	// maxSegmentAgeUnknownDestination is the max age of a segment where we do not have an uploader defined.
	// This is used to prevent segments from being stuck in the queue forever when they'll never get consumed.
	maxSegmentAgeUnknownDestination time.Duration
}

func NewDispatcher(uploaders []Uploader) *dispatcher {
	d := &dispatcher{
		uploaders:                       make(map[string]Uploader),
		queue:                           make(chan *cluster.Batch, 10000),
		maxSegmentAgeUnknownDestination: 12 * time.Hour,
	}
	for _, u := range uploaders {
		logger.Infof("Registering uploader for database %s", u.Database())
		d.uploaders[u.Database()] = u
	}
	return d
}

func (d *dispatcher) Open(ctx context.Context) error {
	c, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	for _, u := range d.uploaders {
		if err := u.Open(c); err != nil {
			return err
		}
	}

	go d.upload(c)
	return nil
}

func (d *dispatcher) Close() error {
	d.cancel()
	for _, u := range d.uploaders {
		u.Close()
	}
	return nil
}

func (d *dispatcher) UploadQueue() chan *cluster.Batch {
	return d.queue
}

func (d *dispatcher) Database() string {
	return ""
}

func (d *dispatcher) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case batch := <-d.queue:
			u, ok := d.uploaders[batch.Database]
			if !ok {
				logger.Errorf("No uploader for database %s", batch.Database)
				d.deleteOldUnknownSegments(batch)
				continue
			}

			select {
			case u.UploadQueue() <- batch:
			default:
				logger.Errorf("Failed to queue batch for %s. Queue is full: %d/%d", batch.Database, len(u.UploadQueue()), cap(u.UploadQueue()))
			}
		}
	}
}

func (d *dispatcher) deleteOldUnknownSegments(batch *cluster.Batch) {
	for _, segment := range batch.Paths {
		segmentCreation, err := cluster.SegmentCreationTime(segment)
		if err != nil {
			continue
		}
		if time.Since(segmentCreation) > d.maxSegmentAgeUnknownDestination {
			logger.Warnf("Segment %s is too old to be uploaded. Will be deleted.", segment)
			os.Remove(segment)
		}
	}
}
