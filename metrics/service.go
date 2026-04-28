package metrics

import "time"

// HealthReporter exposes health and queue/segment status used by the
// per-process metrics collection services in the collector and ingestor.
type HealthReporter interface {
	IsHealthy() bool
	TransferQueueSize() int
	UploadQueueSize() int
	SegmentsTotal() int64
	SegmentsSize() int64
	UnhealthyReason() string
	MaxSegmentAge() time.Duration
}
