package cluster

import (
	"context"
	"sync"
	"time"
)

const (
	ReasonLargeUploadQueue     = "LargeUploadQueue"
	ReasonLargeTransferQueue   = "LargeTransferQueue"
	ReasonMaxSegmentsExceeded  = "MaxSegmentsExceeded"
	ReasonMaxDiskUsageExceeded = "MaxDiskUsageExceeded"
)

// Health tracks the health of peers in the cluster.  If a peer is overloaded, it will be marked as unhealthy
// which will cause the service to stop sending writes to that peer for timeout period.  Similarly, the
// of the current peer is tracked here and if it is unhealthy, the service will stop accepting writes.
type Health struct {
	opts       HealthOpts
	QueueSizer QueueSizer

	mu    sync.RWMutex
	state map[string]*HealthStatus
}

type HealthStatus struct {
	Healthy   bool
	NextCheck time.Time
}

type HealthOpts struct {
	// UnhealthyTimeout is the amount of time to wait before marking a peer as healthy.
	UnhealthyTimeout time.Duration

	QueueSizer      QueueSizer
	MaxSegmentCount int64
	MaxDiskUsage    int64
}

type PeerHealthReporter interface {
	IsPeerHealthy(peer string) bool
	SetPeerUnhealthy(peer string)
	SetPeerHealthy(peer string)
}

type QueueSizer interface {
	TransferQueueSize() int
	UploadQueueSize() int
	SegmentsTotal() int64
	SegmentsSize() int64
}

func NewHealth(opts HealthOpts) *Health {
	if opts.UnhealthyTimeout.Seconds() == 0 {
		opts.UnhealthyTimeout = time.Minute
	}

	h := &Health{
		opts:       opts,
		QueueSizer: opts.QueueSizer,
		state:      make(map[string]*HealthStatus),
	}
	return h
}

func (h *Health) Open(ctx context.Context) error {
	return nil
}

func (h *Health) Close() error {
	return nil
}

func (h *Health) IsHealthy() bool {
	return h.UnhealthyReason() == ""
}

func (h *Health) UnhealthyReason() string {
	uploadQueue := h.QueueSizer.UploadQueueSize()
	transferQueue := h.QueueSizer.TransferQueueSize()

	segmentsTotal := h.QueueSizer.SegmentsTotal()
	segmentsSize := h.QueueSizer.SegmentsSize()

	if uploadQueue >= 5000 {
		return ReasonLargeUploadQueue
	}

	if transferQueue >= 5000 {
		return ReasonLargeTransferQueue
	}

	if segmentsTotal >= h.opts.MaxSegmentCount {
		return ReasonMaxSegmentsExceeded
	}

	if segmentsSize >= h.opts.MaxDiskUsage {
		return ReasonMaxDiskUsageExceeded
	}

	return ""
}

func (h *Health) IsPeerHealthy(peer string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.state[peer]

	// We don't know about this peer, so assume it's healthy.
	if s == nil {
		return true
	}

	return s.Healthy || (s.NextCheck.IsZero() || time.Now().UTC().After(s.NextCheck))
}

func (h *Health) SetPeerUnhealthy(peer string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.state[peer]
	if s == nil {
		s = &HealthStatus{}
	}

	s.Healthy = false
	s.NextCheck = time.Now().UTC().Add(h.opts.UnhealthyTimeout)
	h.state[peer] = s
}

func (h *Health) SetPeerHealthy(peer string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.state[peer]
	if s == nil {
		s = &HealthStatus{}
	}

	s.Healthy = true
	s.NextCheck = time.Time{}
	h.state[peer] = s
}

func (h *Health) UploadQueueSize() int {
	return h.QueueSizer.UploadQueueSize()
}

func (h *Health) TransferQueueSize() int {
	return h.QueueSizer.TransferQueueSize()
}

func (h *Health) SegmentsTotal() int64 {
	return h.QueueSizer.SegmentsTotal()
}

func (h *Health) SegmentsSize() int64 {
	return h.QueueSizer.SegmentsSize()
}
