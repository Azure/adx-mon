package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
)

type ReplicatorOpts struct {
	// Partitioner is used to determine which node owns a given metric.
	Partitioner MetricPartitioner

	// Health is used to report the health of the peer replication.
	Health PeerHealthReporter

	// SegmentRemover is used to remove segments after they have been replicated.
	SegmentRemover SegmentRemover

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// Hostname is the name of the current node.
	Hostname string

	// MaxTransferConcurrency is the maximum number of concurrent transfer requests to in flight at a time.
	// Default is 5.
	MaxTransferConcurrency int

	// DisableGzip controls whether the client uses gzip compression for transfer requests.
	DisableGzip bool
}

type SegmentRemover interface {
	Remove(path string) error
}

// Replicator manages the transfer of local segments to other nodes.
type Replicator interface {
	service.Component
	// TransferQueue returns a channel that can be used to transfer files to other nodes.
	TransferQueue() chan *Batch
}

type replicator struct {
	queue   chan *Batch
	cli     *Client
	wg      sync.WaitGroup
	closeFn context.CancelFunc

	hostname string

	// Partitioner is used to determine which node owns a given metric.
	Partitioner         MetricPartitioner
	Health              PeerHealthReporter
	SegmentRemover      SegmentRemover
	transferConcurrency int
}

func NewReplicator(opts ReplicatorOpts) (Replicator, error) {
	transferConcurrency := 5
	if opts.MaxTransferConcurrency > 0 {
		transferConcurrency = opts.MaxTransferConcurrency
	}

	cli, err := NewClient(ClientOpts{
		Timeout:               30 * time.Second,
		InsecureSkipVerify:    opts.InsecureSkipVerify,
		Close:                 false,
		MaxIdleConnsPerHost:   1,
		MaxIdleConns:          transferConcurrency,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DisableHTTP2:          true,
		DisableKeepAlives:     false,
		DisableGzip:           opts.DisableGzip,
	})
	if err != nil {
		return nil, err
	}

	return &replicator{
		queue:               make(chan *Batch, 10000),
		cli:                 cli,
		hostname:            opts.Hostname,
		Partitioner:         opts.Partitioner,
		Health:              opts.Health,
		SegmentRemover:      opts.SegmentRemover,
		transferConcurrency: transferConcurrency,
	}, nil
}

func (r *replicator) Open(ctx context.Context) error {
	ctx, r.closeFn = context.WithCancel(ctx)
	r.wg.Add(r.transferConcurrency)
	for i := 0; i < r.transferConcurrency; i++ {
		go r.transfer(ctx)
	}
	return nil
}

func (r *replicator) Close() error {
	r.closeFn()
	r.wg.Wait()
	return nil
}

func (r *replicator) TransferQueue() chan *Batch {
	return r.queue
}

func (r *replicator) transfer(ctx context.Context) {
	defer r.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-r.queue:
			segments := batch.Segments

			if err := func() error {
				paths := make([]string, len(segments))
				for i, seg := range segments {
					paths[i] = seg.Path
				}
				mr, err := wal.NewSegmentMerger(paths...)
				if err != nil && os.IsNotExist(err) {
					return nil
				} else if err != nil {
					return fmt.Errorf("open segments: %w", err)
				}
				defer mr.Close()

				// Merge batch of files into the first file at the destination.  This ensures we transfer
				// the full batch atomimcally.
				filename := filepath.Base(paths[0])

				db, table, schema, _, err := wal.ParseFilename(filename)
				if err != nil {
					return fmt.Errorf("parse segment filename: %w", err)
				}

				var key string
				if schema != "" {
					key = fmt.Sprintf("%s_%s_%s", db, table, schema)
				} else {
					key = fmt.Sprintf("%s_%s", db, table)
				}

				// Each metric is written to a distinct file.  The first part of the filename
				// is the metric name.  We use the metric name to determine which node owns
				// the metric.
				owner, addr := r.Partitioner.Owner([]byte(key))

				// We're the owner of the file... leave it for the ingestor to upload.
				if owner == r.hostname {
					return nil
				}

				// If the peer is not healthy, don't attempt transferring the segment.  This could happen if we marked
				// the peer unhealthy after we received the batch to process.
				if !r.Health.IsPeerHealthy(owner) {
					return nil
				}

				start := time.Now()
				if err = r.cli.Write(ctx, addr, filename, mr); err != nil {
					if errors.Is(err, ErrBadRequest{}) {
						// If ingestor returns a bad request, we should drop the segments as it means we're sending something
						// that won't be accepted.  Retrying will continue indefinitely.  In this case, just drop the file
						// and log the error.
						logger.Errorf("Failed to transfer segment %s to %s@%s: %s.  Dropping segments.", filename, owner, addr, err)
						if err := batch.Remove(); err != nil {
							logger.Errorf("Failed to remove segment: %s", err)
						}
						return nil
					} else if errors.Is(err, ErrSegmentExists) {
						// Segment already exists, remove our side so we don't keep retrying.
						if err := batch.Remove(); err != nil {
							logger.Errorf("Failed to remove segment: %s", err)
						}
						return nil
					} else if errors.Is(err, ErrSegmentLocked) {
						// Segment is locked, retry later.
						return nil
					} else if errors.Is(err, ErrPeerOverloaded) {
						// Handle peer overloaded (429) in failover mode: drop segment if database is in failover and log/metric
						if errors.Is(err, ErrPeerOverloaded) {
							// Check if partitioner is failover-aware and if db is in failover
							if fap, ok := r.Partitioner.(*FailoverAwarePartitioner); ok {
								db, _, _, _, _ := wal.ParseFilename(filename)
								if fap.FailoverState != nil && fap.FailoverState.InFailover(db) && owner == fap.FailoverState.GetFailover(db) {
									logger.Warnf("Dropping segment %s for db %s due to failover 429 from sacrificial instance %s", filename, db, owner)
									// TODO: metrics.IngestorSegmentsDroppedFailover.Inc() (add metric)
									if err := batch.Remove(); err != nil {
										logger.Errorf("Failed to remove segment: %s", err)
									}
									return nil
								}
							}
							// Ingestor is overloaded, mark the peer as unhealthy and retry later.
							r.Health.SetPeerUnhealthy(owner)
							return fmt.Errorf("transfer segment %s to %s: %w", filename, addr, err)
						}
						// Unknown error, assume it's transient and retry after some backoff.
						r.Health.SetPeerUnhealthy(owner)
						return err
					}

					for _, seg := range segments {
						if logger.IsDebug() {
							logger.Debugf("Transferred %s as %s to %s addr=%s duration=%s ", seg.Path, filename, owner, addr, time.Since(start).String())
						}
					}
					if err := batch.Remove(); err != nil {
						logger.Errorf("Failed to batch segment: %s", err)
					}

					return nil
				}(); err != nil {
					logger.Errorf("Failed to transfer batch: %v", err)
				}

				batch.Release()
			}
		}
	}
}
