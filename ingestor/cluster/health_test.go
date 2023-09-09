package cluster_test

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/stretchr/testify/require"
)

func TestHealth_IsPeerHealthy(t *testing.T) {
	h := cluster.NewHealth(cluster.HealthOpts{
		QueueSizer: &fakeQueueSizer{},
	})
	require.True(t, h.IsPeerHealthy("ingestor-0"))
}

func TestHealth_SetPeerHealthy(t *testing.T) {
	h := cluster.NewHealth(cluster.HealthOpts{
		UnhealthyTimeout: 100 * time.Millisecond,
		QueueSizer:       &fakeQueueSizer{},
	})
	require.True(t, h.IsPeerHealthy("ingestor-0"))

	h.SetPeerUnhealthy("ingestor-0")
	require.False(t, h.IsPeerHealthy("ingestor-0"))

	time.Sleep(100 * time.Millisecond)

	require.True(t, h.IsPeerHealthy("ingestor-0"))

	h.SetPeerUnhealthy("ingestor-0")
	require.False(t, h.IsPeerHealthy("ingestor-0"))
	h.SetPeerHealthy("ingestor-0")
	require.True(t, h.IsPeerHealthy("ingestor-0"))
}

func TestHealth_IsHealthy(t *testing.T) {
	h := cluster.NewHealth(cluster.HealthOpts{
		QueueSizer:      &fakeQueueSizer{},
		MaxDiskUsage:    1000,
		MaxSegmentCount: 10,
	})
	require.True(t, h.IsHealthy())

	h = cluster.NewHealth(cluster.HealthOpts{
		QueueSizer:      &fakeQueueSizer{transferQueueSize: 3000, uploadQueueSize: 200},
		MaxDiskUsage:    1000,
		MaxSegmentCount: 10,
	})
	require.True(t, h.IsHealthy())

	h = cluster.NewHealth(cluster.HealthOpts{
		QueueSizer:      &fakeQueueSizer{transferQueueSize: 10000},
		MaxDiskUsage:    1000,
		MaxSegmentCount: 10,
	})
	require.False(t, h.IsHealthy())

	h = cluster.NewHealth(cluster.HealthOpts{
		QueueSizer:      &fakeQueueSizer{uploadQueueSize: 10000},
		MaxDiskUsage:    1000,
		MaxSegmentCount: 10,
	})
	require.False(t, h.IsHealthy())

}

type fakeQueueSizer struct {
	uploadQueueSize   int
	transferQueueSize int
	segmentsSize      int64
	segmentsTotal     int64
}

func (f fakeQueueSizer) TransferQueueSize() int { return f.transferQueueSize }
func (f fakeQueueSizer) UploadQueueSize() int   { return f.uploadQueueSize }
func (f fakeQueueSizer) SegmentsSize() int64    { return f.segmentsSize }
func (f fakeQueueSizer) SegmentsTotal() int64   { return f.segmentsTotal }
