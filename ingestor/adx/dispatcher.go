package adx

import (
	"context"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
)

type dispatcher struct {
	uploaders     map[string]Uploader
	queue         chan *cluster.Batch
	realtimeQueue chan *cluster.Batch
	cancel        context.CancelFunc
}

func NewDispatcher(uploaders []Uploader) *dispatcher {
	d := &dispatcher{
		uploaders:     make(map[string]Uploader),
		queue:         make(chan *cluster.Batch, 10000),
		realtimeQueue: make(chan *cluster.Batch, 10000),
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

func (d *dispatcher) RealtimeUploadQueue() chan *cluster.Batch {
	return d.realtimeQueue
}

func (d *dispatcher) Database() string {
	return ""
}

func (d *dispatcher) Endpoint() string {
	return ""
}

func (d *dispatcher) Mgmt(ctx context.Context, query azkustodata.Statement, options ...azkustodata.QueryOption) (kustov1.Dataset, error) {
	// Not implemented.  Should this fanout to all uploaders?
	return nil, nil
}

func (d *dispatcher) upload(ctx context.Context) {
	// Realtime batches are dispatched before queued batches.
	queues := cluster.PriorityQueues{Realtime: d.realtimeQueue, Queued: d.queue}
	for {
		batch, ok := queues.Next(ctx, false)
		if !ok {
			return
		}

		u, ok := d.uploaders[batch.Database]
		if !ok {
			logger.Errorf("No uploader for database %s", batch.Database)
			continue
		}

		queue := u.UploadQueue()
		if batch.Priority == ingestpolicy.PriorityRealtime {
			queue = u.RealtimeUploadQueue()
		}

		select {
		case queue <- batch:
		default:
			batch.Release()
			logger.Errorf("Failed to queue batch for %s. Queue is full: %d/%d", batch.Database, len(queue), cap(queue))
		}
	}
}
