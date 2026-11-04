package ingestor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

var (
	// forwardTimeout bounds forwarding a transfer to its owner.  It must fit within the time the collector waits for a
	// response so a slow owner falls back to storing the transfer locally instead of failing the collector's request.
	forwardTimeout = 3 * time.Second

	// forwardSlotsPerPeer bounds concurrent forwards to one owner to its connection limit, so a forward never waits
	// for a connection.
	forwardSlotsPerPeer = 5
)

type transferWriter interface {
	Write(ctx context.Context, endpoint string, filename string, body io.Reader) error
}

// transferForwarder forwards collector transfers for tables owned by another ingestor to the owner, so they are not
// stored here and transferred again after this node's segment rotates.
type transferForwarder struct {
	hostname    string
	partitioner cluster.MetricPartitioner
	health      cluster.PeerHealthReporter
	client      transferWriter

	mu    sync.Mutex
	slots map[string]chan struct{}
}

func newTransferForwarder(hostname string, partitioner cluster.MetricPartitioner, health cluster.PeerHealthReporter, insecureSkipVerify bool) (*transferForwarder, error) {
	client, err := cluster.NewClient(cluster.ClientOpts{
		Timeout:               forwardTimeout,
		ResponseHeaderTimeout: forwardTimeout,
		InsecureSkipVerify:    insecureSkipVerify,
		MaxConnsPerHost:       forwardSlotsPerPeer,
		MaxIdleConnsPerHost:   forwardSlotsPerPeer,
		DisableHTTP2:          true,
		DisableGzip:           true,
		PeerTransfer:          true,
	})
	if err != nil {
		return nil, err
	}
	return &transferForwarder{hostname: hostname, partitioner: partitioner, health: health, client: client, slots: make(map[string]chan struct{})}, nil
}

// forward sends a transfer to its table's owner and reports whether the owner accepted it.  It returns false without
// forwarding when this node owns the table, and when the owner is unhealthy, busy or fails, so the transfer is stored
// locally as it would be without forwarding.
func (f *transferForwarder) forward(filename string, data []byte) bool {
	db, table, schema, _, err := wal.ParseFilename(filename)
	if err != nil {
		return false
	}
	key := fmt.Sprintf("%s_%s", db, table)
	if schema != "" {
		key = fmt.Sprintf("%s_%s_%s", db, table, schema)
	}
	owner, addr := f.partitioner.Owner([]byte(key))
	if owner == f.hostname || owner == "" {
		return false
	}
	if !f.health.IsPeerHealthy(owner) {
		metrics.IngestorTransferForwards.WithLabelValues("fallback_unhealthy").Inc()
		return false
	}

	slots := f.peerSlots(owner)
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		metrics.IngestorTransferForwards.WithLabelValues("fallback_busy").Inc()
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), forwardTimeout)
	defer cancel()
	if err := f.client.Write(ctx, addr, filename, bytes.NewReader(data)); err != nil {
		metrics.IngestorTransferForwards.WithLabelValues("fallback_error").Inc()
		var quota *wal.TableOverloadedError
		var tooLarge *wal.SegmentTooLargeError
		var overloaded *cluster.PeerOverloadedError
		switch {
		case errors.As(err, &quota), errors.As(err, &tooLarge), errors.Is(err, cluster.ErrSegmentExists),
			errors.Is(err, cluster.ErrSegmentLocked), errors.Is(err, &cluster.ErrBadRequest{}):
			// The owner rejected this transfer but is healthy.
		case errors.As(err, &overloaded):
			f.health.SetPeerUnhealthyFor(owner, overloaded.RetryAfter)
		default:
			f.health.SetPeerUnhealthy(owner)
		}
		logger.Warnf("Failed to forward transfer %s to %s, storing locally: %s", filename, owner, err)
		return false
	}
	metrics.IngestorTransferForwards.WithLabelValues("forwarded").Inc()
	return true
}

func (f *transferForwarder) peerSlots(peer string) chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	slots, ok := f.slots[peer]
	if !ok {
		slots = make(chan struct{}, forwardSlotsPerPeer)
		f.slots[peer] = slots
	}
	return slots
}
