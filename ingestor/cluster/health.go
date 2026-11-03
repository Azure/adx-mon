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
	Failures  int
}

type HealthOpts struct {
	// UnhealthyTimeout is the maximum amount of time to wait before marking a peer as healthy.
	UnhealthyTimeout time.Duration

	// InitialUnhealthyTimeout is how long a peer is skipped after its first failure.  Each consecutive failure
	// doubles it, up to UnhealthyTimeout, and a success resets it.  Defaults to UnhealthyTimeout, which skips the
	// peer for the full UnhealthyTimeout on every failure.
	InitialUnhealthyTimeout time.Duration

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
	MaxSegmentAge() time.Duration
}

func NewHealth(opts HealthOpts) *Health {
	if opts.UnhealthyTimeout.Seconds() == 0 {
		opts.UnhealthyTimeout = time.Minute
	}
	if opts.InitialUnhealthyTimeout <= 0 || opts.InitialUnhealthyTimeout > opts.UnhealthyTimeout {
		opts.InitialUnhealthyTimeout = opts.UnhealthyTimeout
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
	segmentsTotal := h.QueueSizer.SegmentsTotal()
	segmentsSize := h.QueueSizer.SegmentsSize()

	if h.opts.MaxSegmentCount > 0 && segmentsTotal >= h.opts.MaxSegmentCount {
		return ReasonMaxSegmentsExceeded
	}

	if h.opts.MaxDiskUsage > 0 && segmentsSize >= h.opts.MaxDiskUsage {
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

	now := time.Now().UTC()
	if !s.Healthy && s.Failures > 0 && now.Before(s.NextCheck) {
		// A failure while the peer is already backing off, such as another in-flight request, restarts the current
		// backoff without escalating it.
		s.NextCheck = now.Add(h.peerBackoff(s.Failures - 1))
	} else {
		s.NextCheck = now.Add(h.peerBackoff(s.Failures))
		s.Failures++
	}

	s.Healthy = false
	h.state[peer] = s
}

// peerBackoff returns InitialUnhealthyTimeout doubled once per prior failure, capped at UnhealthyTimeout.
func (h *Health) peerBackoff(failures int) time.Duration {
	d := h.opts.InitialUnhealthyTimeout
	for i := 0; i < failures && d < h.opts.UnhealthyTimeout; i++ {
		d *= 2
	}
	return min(d, h.opts.UnhealthyTimeout)
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
	s.Failures = 0
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

func (h *Health) MaxSegmentAge() time.Duration {
	return h.QueueSizer.MaxSegmentAge()
}
