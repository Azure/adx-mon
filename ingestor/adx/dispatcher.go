package adx

import (
	"context"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
)

type dispatcher struct {
	uploaders map[string]Uploader
	queue     chan *cluster.Batch
	cancel    context.CancelFunc
}

func NewDispatcher(uploaders []Uploader) *dispatcher {
	return &dispatcher{
		uploaders: make(map[string]Uploader),
		queue:     make(chan *cluster.Batch, 10000),
	}
}

func (d *dispatcher) Open(ctx context.Context) error {
	c, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	for _, u := range d.uploaders {
		d.uploaders[u.Database()] = u
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
				logger.Error("No uploader for database %s", batch.Database)
				continue
			}

			select {
			case u.UploadQueue() <- batch:
			default:
				logger.Error("Failed to queue batch for %s. Queue is full", batch.Database)
			}
		}
	}
}
