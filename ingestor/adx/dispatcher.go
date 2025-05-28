package adx

import (
	"context"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
)

type dispatcher struct {
	uploaders map[string]Uploader
	queue     chan *cluster.Batch
	cancel    context.CancelFunc
}

func NewDispatcher(uploaders []Uploader) *dispatcher {
	d := &dispatcher{
		uploaders: make(map[string]Uploader),
		queue:     make(chan *cluster.Batch, 10000),
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

func (d *dispatcher) Endpoint() string {
	return ""
}

func (d *dispatcher) Mgmt(ctx context.Context, query kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	// Not implemented.  Should this fanout to all uploaders?
	return nil, nil
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
				continue
			}

			select {
			case u.UploadQueue() <- batch:
			default:
				batch.Release()
				logger.Errorf("Failed to queue batch for %s. Queue is full: %d/%d", batch.Database, len(u.UploadQueue()), cap(u.UploadQueue()))
			}
		}
	}
}
