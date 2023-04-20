package cluster

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ReplicatorOpts struct {
	// Partitioner is used to determine which node owns a given metric.
	Partitioner MetricPartitioner
}

// Replicator manages the transfer of local segments to other nodes.
type Replicator interface {
	service.Component
	// TransferQueue returns a channel that can be used to transfer files to other nodes.
	TransferQueue() chan []string
}

type replicator struct {
	queue   chan []string
	cli     *Client
	wg      sync.WaitGroup
	closing chan struct{}

	hostname string

	// Partitioner is used to determine which node owns a given metric.
	Partitioner MetricPartitioner
}

func NewReplicator(opts ReplicatorOpts) (Replicator, error) {
	cli, err := NewClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	return &replicator{
		queue:       make(chan []string, 100),
		closing:     make(chan struct{}),
		cli:         cli,
		Partitioner: opts.Partitioner,
	}, nil
}

func (r *replicator) Open(ctx context.Context) error {
	go r.transfer()
	return nil
}

func (r *replicator) Close() error {
	close(r.closing)
	r.wg.Wait()
	return nil
}

func (r *replicator) TransferQueue() chan []string {
	return r.queue
}

func (r *replicator) transfer() {
	r.wg.Add(1)
	defer r.wg.Done()

	for {
		select {
		case <-r.closing:
			return
		case segments := <-r.queue:
			for _, seg := range segments {
				filename := filepath.Base(seg)
				parts := strings.Split(filename, "_")

				// Each metric is written to a distinct file.  The first part of the filename
				// is the metric name.  We use the metric name to determine which node owns
				// the metric.
				owner, addr := r.Partitioner.Owner([]byte(parts[0]))

				// We're the owner of the file... leave it for the ingestor to upload.
				if owner == r.hostname {
					continue
				}

				start := time.Now()
				if err := r.cli.Write(context.Background(), addr, seg); err != nil {
					logger.Error("Failed to transfer segment %s to %s: %v", seg, addr, err)
					continue
				}
				if err := os.Remove(seg); err != nil {
					logger.Error("Failed to remove segment %s: %v", seg, err)
					continue
				}
				logger.Info("Segment %s transferred to %s duration=%s ", seg, addr, time.Since(start).String())

			}
		}
	}
}
