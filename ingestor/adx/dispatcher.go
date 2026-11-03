package adx

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/azkustodata"
	kustov1 "github.com/Azure/azure-kusto-go/azkustodata/query/v1"
)

// unroutableRetryDelay is how long a batch for a database without an uploader is held before it is released.  The set
// of uploaders is fixed until restart, so the batch cannot be routed sooner; holding it keeps the batcher from
// rebuilding and redispatching it on every sweep.
const unroutableRetryDelay = time.Minute

type dispatcher struct {
	uploaders     map[string]Uploader
	queue         chan *cluster.Batch
	realtimeQueue chan *cluster.Batch
	cancel        context.CancelFunc

	unroutableDelay time.Duration

	// unroutableLogged is when a missing uploader was last logged for each database.
	mu               sync.Mutex
	unroutableLogged map[string]time.Time
}

func NewDispatcher(uploaders []Uploader) *dispatcher {
	d := &dispatcher{
		uploaders:     make(map[string]Uploader),
		queue:         make(chan *cluster.Batch, 10000),
		realtimeQueue: make(chan *cluster.Batch, 10000),

		unroutableDelay:  unroutableRetryDelay,
		unroutableLogged: make(map[string]time.Time),
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
	// Dispatch the queues independently so a busy or congested realtime path cannot prevent queued batches from
	// reaching uploaders with workers reserved for them.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		d.dispatch(ctx, d.realtimeQueue)
	}()
	go func() {
		defer wg.Done()
		d.dispatch(ctx, d.queue)
	}()
	wg.Wait()
}

func (d *dispatcher) dispatch(ctx context.Context, input <-chan *cluster.Batch) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case <-ctx.Done():
			return
		case batch := <-input:
			d.dispatchBatch(batch)
		}
	}
}

func (d *dispatcher) dispatchBatch(batch *cluster.Batch) {
	u, ok := d.uploaders[batch.Database]
	if !ok {
		d.unroutable(batch)
		return
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

// unroutable handles a batch for a database without an uploader.  The batch is released after a delay so its segments
// are not left marked as batched forever: an unreleased batch is never batched again, uploaded or removed, so its
// segments stay on disk and the oldest segment age grows without bound.
func (d *dispatcher) unroutable(batch *cluster.Batch) {
	now := time.Now()
	d.mu.Lock()
	logIt := now.Sub(d.unroutableLogged[batch.Database]) >= d.unroutableDelay
	if logIt {
		d.unroutableLogged[batch.Database] = now
	}
	d.mu.Unlock()
	if logIt {
		logger.Errorf("No uploader for database %s; retrying its batches every %s", batch.Database, d.unroutableDelay)
	}

	if d.unroutableDelay <= 0 {
		batch.Release()
		return
	}
	time.AfterFunc(d.unroutableDelay, batch.Release)
}
