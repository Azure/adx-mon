package cluster

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ReplicatorOpts struct {
	Partitioner MetricPartitioner
}

type Replicator struct {
	queue   chan []string
	cli     *Client
	wg      sync.WaitGroup
	closing chan struct{}

	hostname    string
	Partitioner MetricPartitioner
}

func NewReplicator(opts ReplicatorOpts) (*Replicator, error) {
	cli, err := NewClient(30 * time.Second)
	if err != nil {
		return nil, err
	}
	return &Replicator{
		queue:       make(chan []string, 100),
		cli:         cli,
		Partitioner: opts.Partitioner,
	}, nil
}

func (r *Replicator) Open() error {
	go r.transfer()
	return nil
}

func (r *Replicator) Close() error {
	close(r.closing)
	r.wg.Wait()
	return nil
}

func (r *Replicator) TransferQueue() chan []string {
	return r.queue
}

func (r *Replicator) transfer() {
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
				owner, addr := r.Partitioner.Owner([]byte(parts[0]))

				// We're not the owner of the file... leave it for the ingestor to upload
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
