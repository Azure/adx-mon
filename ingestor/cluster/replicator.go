package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pkg/wal"
	"go.uber.org/multierr"
)

// undeliverableBackoff is how long a destination waits after a payload it can
// never accept, so an unresolvable batch cannot spin.
const undeliverableBackoff = 5 * time.Minute

// maxSchedulerRetry bounds how long the scheduler defers a destination on
// behalf of one peer. The per-peer backoff remains authoritative.
const maxSchedulerRetry = time.Second

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

	// QueuedReservedWorkersPercent is the percentage of transfer workers reserved for queued batches.  Defaults to
	// DefaultQueuedReservedWorkersPercent.
	QueuedReservedWorkersPercent int
}

type SegmentRemover interface {
	Remove(path string) error
}

// Replicator manages the transfer of local segments to other nodes.
type Replicator interface {
	service.Component
	// TransferQueue returns a channel that can be used to transfer files to other nodes.
	TransferQueue() chan *Batch

	// RealtimeTransferQueue returns a channel for realtime batches.  They are transferred before queued batches.
	RealtimeTransferQueue() chan *Batch
}

type replicator struct {
	queue         chan *Batch
	realtimeQueue chan *Batch
	cli           *Client
	wg            sync.WaitGroup
	closeFn       context.CancelFunc

	hostname string

	// Partitioner is used to determine which node owns a given metric.
	Partitioner         MetricPartitioner
	Health              PeerHealthReporter
	SegmentRemover      SegmentRemover
	transferConcurrency int
	reservedPercent     int
	backoff             destinationBackoff
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
		realtimeQueue:       make(chan *Batch, 10000),
		cli:                 cli,
		hostname:            opts.Hostname,
		Partitioner:         opts.Partitioner,
		Health:              opts.Health,
		SegmentRemover:      opts.SegmentRemover,
		transferConcurrency: transferConcurrency,
		reservedPercent:     opts.QueuedReservedWorkersPercent,
	}, nil
}

func (r *replicator) Open(ctx context.Context) error {
	ctx, r.closeFn = context.WithCancel(ctx)
	r.wg.Add(r.transferConcurrency)
	// Queued transfers are scheduled fairly across destinations so one table
	// cannot monopolise the pool. Realtime transfers keep their direct path.
	queued := FairQueue(ctx, r.queue, FairQueueOpts{Workers: r.transferConcurrency, Pipeline: "transfer"})
	queues := PriorityQueues{Realtime: r.realtimeQueue, Queued: queued}
	RunWorkers(ctx, queues, r.transferConcurrency, r.reservedPercent, func(batch *Batch) {
		r.transfer(ctx, batch)
	}, r.wg.Done)
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

func (r *replicator) RealtimeTransferQueue() chan *Batch {
	return r.realtimeQueue
}

func (r *replicator) transfer(ctx context.Context, batch *Batch) {
	if err := r.send(ctx, batch, batch.Segments, 0); err != nil {
		logger.Errorf("Failed to transfer batch to peer: %v", err)
	}

	batch.Release()
}

// maxSplitDepth bounds how many times an oversized batch is halved before it is
// reported as undeliverable.
const maxSplitDepth = 8

func (r *replicator) send(ctx context.Context, batch *Batch, segments []wal.SegmentInfo, depth int) error {
	if len(segments) == 0 {
		return nil
	}

	return func() error {
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
		if !r.backoff.ready(owner, wal.Destination{Database: db, Table: table}, time.Now()) {
			return nil
		}

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
			var quota *wal.TableOverloadedError
			if errors.As(err, &quota) {
				// The authoritative backoff is per peer and destination. The
				// scheduler can only defer by destination, and a destination's
				// schema prefixes may be owned by different peers, so the
				// scheduler pause is kept short to avoid stalling work bound
				// for a healthy peer while still preventing a hot retry loop.
				r.backoff.deferUntil(owner, quota.Destination, time.Now().Add(quota.RetryAfter))
				batch.RetryAfter(min(quota.RetryAfter, maxSchedulerRetry))
				return nil
			}
			var oversized *wal.SegmentTooLargeError
			if errors.As(err, &oversized) {
				return r.splitOversized(ctx, batch, segments, depth, oversized, owner, addr)
			}
			if errors.Is(err, &ErrBadRequest{}) {
				// If ingestor returns a bad request, we should drop the segments as it means we're sending something
				// that won't be accepted.  Retrying will continue indefinitely.  In this case, just drop the file
				// and log the error.
				logger.Errorf("Failed to transfer segment %s to %s@%s: %s.  Dropping segments.", filename, owner, addr, err)
				if err := r.remove(batch, segments); err != nil {
					logger.Errorf("Failed to remove segment: %s", err)
				}
				return nil
			} else if errors.Is(err, ErrSegmentExists) {
				// Segment already exists, remove our side so we don't keep retrying.
				if err := r.remove(batch, segments); err != nil {
					logger.Errorf("Failed to remove segment: %s", err)
				}
				return nil
			} else if errors.Is(err, ErrSegmentLocked) {
				// Segment is locked, retry later.
				return nil
			} else if errors.Is(err, ErrPeerOverloaded) {
				// Ingestor is overloaded, mark the peer as unhealthy and retry later.
				r.Health.SetPeerUnhealthy(owner)
				return fmt.Errorf("transfer segment %s to %s: %w", filename, addr, err)
			}
			// Unknown error, assume it's transient and retry after some backoff.
			r.Health.SetPeerUnhealthy(owner)
			return err
		}

		if logger.IsDebug() {
			for _, seg := range segments {
				logger.Debugf("Transferred %s as %s to %s addr=%s duration=%s ", seg.Path, filename, owner, addr, time.Since(start).String())
			}
		}

		if err := r.remove(batch, segments); err != nil {
			logger.Errorf("Failed to batch segment: %s", err)
		}

		return nil
	}()
}

// splitOversized halves a batch the destination cannot accept and retries each
// half. A single segment that still does not fit cannot be resolved by
// splitting, so it is retained and reported rather than retried forever.
func (r *replicator) splitOversized(ctx context.Context, batch *Batch, segments []wal.SegmentInfo, depth int, oversized *wal.SegmentTooLargeError, owner, addr string) error {
	if len(segments) <= 1 || depth >= maxSplitDepth {
		metrics.IngestorUndeliverableSegments.WithLabelValues(oversized.Destination.Database, oversized.Destination.Table).Inc()
		logger.Errorf("Segment %s cannot be delivered to %s@%s: %s. Raise the destination table budget or lower max-transfer-size; the data is retained locally.",
			segments[0].Path, owner, addr, oversized)
		// Long backoff so an unresolvable payload cannot spin.
		r.backoff.deferUntil(owner, oversized.Destination, time.Now().Add(undeliverableBackoff))
		batch.RetryAfter(undeliverableBackoff)
		return nil
	}

	mid := len(segments) / 2
	logger.Warnf("Splitting oversized transfer for %s into %d and %d segments: %s",
		oversized.Destination, mid, len(segments)-mid, oversized)

	var errs error
	for _, half := range [][]wal.SegmentInfo{segments[:mid], segments[mid:]} {
		if err := r.send(ctx, batch, half, depth+1); err != nil {
			errs = multierr.Append(errs, err)
		}
	}
	return errs
}

// remove deletes the given segments, which may be a subset of the batch when an
// oversized transfer was split.
func (r *replicator) remove(batch *Batch, segments []wal.SegmentInfo) error {
	if len(segments) == len(batch.Segments) {
		return batch.Remove()
	}
	return batch.RemoveSegments(segments)
}
