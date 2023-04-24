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

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// Hostname is the name of the current node.
	Hostname string
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
	closeFn context.CancelFunc

	hostname string

	// Partitioner is used to determine which node owns a given metric.
	Partitioner MetricPartitioner
}

func NewReplicator(opts ReplicatorOpts) (Replicator, error) {
	cli, err := NewClient(30*time.Second, opts.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}
	return &replicator{
		queue:       make(chan []string, 100),
		cli:         cli,
		hostname:    opts.Hostname,
		Partitioner: opts.Partitioner,
	}, nil
}

func (r *replicator) Open(ctx context.Context) error {
	ctx, r.closeFn = context.WithCancel(ctx)
	go r.transfer(ctx)
	return nil
}

func (r *replicator) Close() error {
	r.closeFn()
	r.wg.Wait()
	return nil
}

func (r *replicator) TransferQueue() chan []string {
	return r.queue
}

func (r *replicator) transfer(ctx context.Context) {
	r.wg.Add(1)
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
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
