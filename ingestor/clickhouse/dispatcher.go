package clickhouse

import (
	"context"
	"log/slog"

	"github.com/Azure/adx-mon/ingestor/cluster"
	monlogger "github.com/Azure/adx-mon/pkg/logger"
)

// dispatcher fan-outs batches based on database name so that the ingestor can
// treat ClickHouse uploaders the same way it treats the ADX dispatcher.
type dispatcher struct {
	log       *slog.Logger
	uploaders map[string]Uploader
	queue     chan *cluster.Batch
	cancel    context.CancelFunc
}

// NewDispatcher builds a dispatcher that multiplexes batches to the provided
// ClickHouse uploaders. The caller is responsible for ensuring the uploaders
// target distinct databases.
func NewDispatcher(log *slog.Logger, uploaders []Uploader) Uploader {
	if log == nil {
		log = monlogger.Logger()
	}

	d := &dispatcher{
		log:       log.With(slog.String("component", "clickhouse-dispatcher")),
		uploaders: make(map[string]Uploader, len(uploaders)),
		queue:     make(chan *cluster.Batch, DefaultQueueCapacity),
	}

	for _, u := range uploaders {
		if u == nil {
			continue
		}
		d.log.Info("registering clickhouse uploader", slog.String("database", u.Database()))
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

	go d.dispatch(c)
	return nil
}

func (d *dispatcher) Close() error {
	if d.cancel != nil {
		d.cancel()
	}
	for _, u := range d.uploaders {
		if err := u.Close(); err != nil {
			d.log.Warn("failed to close clickhouse uploader", slog.String("database", u.Database()), slog.String("error", err.Error()))
		}
	}
	return nil
}

func (d *dispatcher) UploadQueue() chan *cluster.Batch {
	return d.queue
}

func (d *dispatcher) Database() string {
	return ""
}

func (d *dispatcher) DSN() string {
	return ""
}

func (d *dispatcher) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-d.queue:
			if batch == nil {
				continue
			}
			uploader, ok := d.uploaders[batch.Database]
			if !ok {
				d.log.Error("no clickhouse uploader registered for batch", slog.String("database", batch.Database))
				batch.Release()
				continue
			}
			select {
			case uploader.UploadQueue() <- batch:
			default:
				batch.Release()
				d.log.Error("clickhouse uploader queue is full", slog.String("database", batch.Database), slog.Int("queue_depth", len(uploader.UploadQueue())))
			}
		}
	}
}
