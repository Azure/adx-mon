package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
)

type fairTable struct {
	queue     []*Batch
	running   int
	deficit   int64
	lastRound int
	retryAt   time.Time
}

type completion struct {
	destination wal.Destination
	delay       time.Duration
}

// FairQueueOpts configures how queued work is shared between destinations.
type FairQueueOpts struct {
	// Workers is the size of the consuming worker pool.
	Workers int

	// Pipeline labels the exported scheduling metrics. Empty disables them.
	Pipeline string

	// Quantum is the byte credit a destination earns per round. Larger values
	// favour throughput, smaller values favour fairness between destinations
	// with very different batch sizes.
	Quantum int64

	// QueueDepth bounds how many batches are held per destination. Excess
	// batches are released and rediscovered by the next WAL scan.
	QueueDepth int

	// ReservedWorkers is how many workers stay available for destinations that
	// are not already running. Without a reserve, a destination with enough
	// queued work saturates the pool before a new destination appears and the
	// newcomer waits for an in-flight batch to finish. Defaults to one when the
	// pool has more than one worker.
	ReservedWorkers int
}

func (o FairQueueOpts) withDefaults() FairQueueOpts {
	if o.Workers < 1 {
		o.Workers = 1
	}
	if o.Quantum <= 0 {
		o.Quantum = 1024 * 1024
	}
	if o.QueueDepth <= 0 {
		o.QueueDepth = max(2, o.Workers*2)
	}
	if o.ReservedWorkers <= 0 && o.Workers > 1 {
		o.ReservedWorkers = 1
	}
	if o.ReservedWorkers >= o.Workers {
		o.ReservedWorkers = o.Workers - 1
	}
	return o
}

// FairQueue drains a compatibility input channel into bounded per-table queues
// and schedules them with deficit round robin, so a destination's share of
// throughput reflects bytes rather than batch count.
//
// A full table queue releases its claims for a later WAL scan, never its data.
// Batches must be released by workers. A destination may use the whole pool
// when it is the only one with work; as other destinations become active each
// one keeps a worker in reserve, so no destination can monopolise the pool.
//
// The returned channel plugs into PriorityQueues as the queued (bulk) path.
// Realtime work is deliberately not scheduled here: it is latency critical and
// already protected by reserved workers.
func FairQueue(ctx context.Context, input <-chan *Batch, opts FairQueueOpts) chan *Batch {
	opts = opts.withDefaults()
	output := make(chan *Batch)
	go runFairQueue(ctx, input, output, opts)
	return output
}

func runFairQueue(ctx context.Context, input <-chan *Batch, output chan *Batch, opts FairQueueOpts) {
	defer close(output)

	done := make(chan completion, opts.Workers)
	tables := make(map[wal.Destination]*fairTable)
	var order []wal.Destination
	cursor, round := 0, 0

	var statusMu sync.Mutex
	status := make(map[wal.Destination]metrics.TableWork)
	statusDirty := true
	if opts.Pipeline != "" {
		defer metrics.RegisterTableWork(opts.Pipeline, func() map[wal.Destination]metrics.TableWork {
			statusMu.Lock()
			defer statusMu.Unlock()
			result := make(map[wal.Destination]metrics.TableWork, len(status))
			for d, s := range status {
				result[d] = s
			}
			return result
		})()
	}

	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	defer func() {
		for _, table := range tables {
			for _, b := range table.queue {
				b.mu.Lock()
				b.completion = nil
				b.mu.Unlock()
				b.Release()
			}
		}
		for {
			select {
			case b, ok := <-input:
				if !ok {
					return
				}
				if b != nil {
					b.Release()
				}
			default:
				return
			}
		}
	}()

	// retire drops a destination once it has no work, nothing running and no
	// pending retry, which bounds both scheduling cost and metric cardinality.
	retire := func(d wal.Destination) {
		t := tables[d]
		if t == nil || len(t.queue) > 0 || t.running > 0 || t.retryAt.After(time.Now()) {
			return
		}
		delete(tables, d)
		for i, candidate := range order {
			if candidate == d {
				order = append(order[:i], order[i+1:]...)
				if cursor > i {
					cursor--
				}
				break
			}
		}
		statusDirty = true
	}

	// A worker is held back for destinations that are not already running, so a
	// newcomer can always start without waiting for an in-flight batch.
	limit := opts.Workers - opts.ReservedWorkers
	if limit < 1 {
		limit = 1
	}

	for {
		candidate, destination := selectCandidate(tables, order, &cursor, &round, limit, opts.Quantum)

		var out chan *Batch
		if candidate != nil {
			out = output
			d := destination
			candidate.mu.Lock()
			candidate.completion = func(delay time.Duration) {
				select {
				case done <- completion{d, delay}:
				case <-ctx.Done():
				}
			}
			candidate.mu.Unlock()
		}

		dispatched := false
		select {
		case <-ctx.Done():
			return

		case b, ok := <-input:
			if !ok {
				return
			}
			if b == nil {
				continue
			}
			d := wal.Destination{Database: b.Database, Table: b.Table}
			if d.Database == "" && len(b.Segments) > 0 {
				d, _ = wal.DestinationFromFilename(b.Segments[0].Path)
			}
			table := tables[d]
			if table == nil {
				table = &fairTable{lastRound: round}
				tables[d] = table
				order = append(order, d)
			}
			if len(table.queue) >= opts.QueueDepth {
				b.Release()
				continue
			}
			table.queue = append(table.queue, b)
			statusDirty = true

		case out <- candidate:
			dispatched = true
			table := tables[destination]
			table.queue = table.queue[1:]
			table.deficit -= batchBytes(candidate)
			table.running++
			if len(table.queue) == 0 {
				// No work left: surrender unused credit so an idle destination
				// cannot hoard it and burst later.
				table.deficit = 0
			}
			statusDirty = true

		case c := <-done:
			if table := tables[c.destination]; table != nil {
				table.running--
				if deadline := time.Now().Add(c.delay); c.delay > 0 && deadline.After(table.retryAt) {
					table.retryAt = deadline
				}
				retire(c.destination)
				statusDirty = true
			}

		case <-tick.C:
			// Retry deadlines expire on the tick; retire anything now idle.
			for _, d := range append([]wal.Destination(nil), order...) {
				retire(d)
			}
		}

		if candidate != nil && !dispatched {
			candidate.mu.Lock()
			candidate.completion = nil
			candidate.mu.Unlock()
		}

		if statusDirty && opts.Pipeline != "" {
			statusMu.Lock()
			clear(status)
			for d, t := range tables {
				status[d] = metrics.TableWork{Queued: len(t.queue), Running: t.running, RetryAt: t.retryAt}
			}
			statusMu.Unlock()
			statusDirty = false
		}
	}
}

// selectCandidate implements deficit round robin: each destination earns one
// quantum of credit per round and keeps the cursor while it can afford its next
// batch, so throughput is shared by bytes rather than by batch count.
func selectCandidate(tables map[wal.Destination]*fairTable, order []wal.Destination, cursor, round *int, limit int, quantum int64) (*Batch, wal.Destination) {
	now := time.Now()
	for visited := 0; visited <= len(order); visited++ {
		if len(order) == 0 {
			return nil, wal.Destination{}
		}
		if *cursor >= len(order) {
			*cursor = 0
			*round++
		}

		d := order[*cursor]
		table := tables[d]
		if table == nil || len(table.queue) == 0 || table.running >= limit || table.retryAt.After(now) {
			*cursor++
			continue
		}

		if table.lastRound != *round {
			table.deficit += quantum
			table.lastRound = *round
		}

		if cost := batchBytes(table.queue[0]); cost <= table.deficit {
			return table.queue[0], d
		}

		// Cannot afford its next batch this round; keep the credit and let the
		// next destination run.
		*cursor++
	}
	return nil, wal.Destination{}
}

func batchBytes(b *Batch) int64 {
	var size int64
	for _, s := range b.Segments {
		size += s.Size
	}
	if size < 1 {
		return 1
	}
	return size
}
