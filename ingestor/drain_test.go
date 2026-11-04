package ingestor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// drainUploader exposes queues whose depth the test controls, standing in for
// an uploader that is or is not making progress.
type drainUploader struct {
	Uploader
	queue chan *cluster.Batch
}

func (u *drainUploader) UploadQueue() chan *cluster.Batch { return u.queue }

type drainReplicator struct {
	cluster.Replicator
	queue    chan *cluster.Batch
	realtime chan *cluster.Batch
}

func (r *drainReplicator) TransferQueue() chan *cluster.Batch         { return r.queue }
func (r *drainReplicator) RealtimeTransferQueue() chan *cluster.Batch { return r.realtime }

type drainBatcher struct {
	cluster.Batcher
	segments func() int64
}

func (b *drainBatcher) BatchSegments() error { return nil }

func (b *drainBatcher) SegmentsTotal() int64 { return b.segments() }

func newDrainService(queued int) *Service {
	upload := make(chan *cluster.Batch, 16)
	for n := 0; n < queued; n++ {
		upload <- &cluster.Batch{}
	}

	return &Service{
		uploader:   &drainUploader{queue: upload},
		replicator: &drainReplicator{queue: make(chan *cluster.Batch), realtime: make(chan *cluster.Batch)},
		batcher:    &drainBatcher{segments: func() int64 { return int64(len(upload)) }},
	}
}

// consume drains the upload queue the way workers would, until it is empty.
func consume(t *testing.T, s *Service, every time.Duration) {
	t.Helper()
	go func() {
		queue := s.uploader.UploadQueue()
		for {
			select {
			case <-queue:
				time.Sleep(every)
			default:
				return
			}
		}
	}()
}

// A backlog that cannot drain must not hold shutdown open until the context
// deadline; remaining work stays on disk for the next start.
func TestDrainGivesUpOnStalledBacklog(t *testing.T) {
	previous := stallTimeoutForTest(150 * time.Millisecond)
	defer stallTimeoutForTest(previous)

	s := newDrainService(5)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	require.NoError(t, s.UploadSegments(ctx), "shutdown proceeds rather than blocking")
	require.Less(t, time.Since(start), 5*time.Second, "did not wait for the full deadline")
	require.Equal(t, 5, s.uploadQueueLen(), "undrained work stays queued for the next start")
}

// Progress must reset the stall budget so a slow but advancing drain completes.
func TestDrainWaitsWhileMakingProgress(t *testing.T) {
	previous := stallTimeoutForTest(3 * time.Second)
	defer stallTimeoutForTest(previous)

	s := newDrainService(3)
	consume(t, s, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, s.UploadSegments(ctx))
	require.Zero(t, s.uploadQueueLen(), "backlog drained completely")
}

// An already empty pipeline returns immediately.
func TestDrainReturnsWhenAlreadyEmpty(t *testing.T) {
	s := newDrainService(0)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, s.UploadSegments(ctx))
}

// Batches the upload scheduler has already taken off the upload queue are still pending work, so the drain must wait
// for them to be uploaded rather than return as soon as the queue itself is empty.
func TestDrainWaitsForBatchesHeldByScheduler(t *testing.T) {
	const batches = 5
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Each queued batch is one closed segment that stays pending until a worker uploads it.
	var segments atomic.Int64
	segments.Store(batches)
	upload := make(chan *cluster.Batch, batches)
	scheduled := cluster.FairQueue(ctx, upload, cluster.FairQueueOpts{Workers: 1, QueueDepth: batches})
	go func() {
		for b := range scheduled {
			time.Sleep(100 * time.Millisecond)
			segments.Add(-1)
			b.Release()
		}
	}()
	for i := 0; i < batches; i++ {
		upload <- &cluster.Batch{Database: "DB", Table: "Table", Segments: []wal.SegmentInfo{{Size: 1}}}
	}
	require.Eventually(t, func() bool { return len(upload) == 0 }, time.Second, time.Millisecond)

	s := &Service{
		uploader:   &drainUploader{queue: upload},
		replicator: &drainReplicator{queue: make(chan *cluster.Batch), realtime: make(chan *cluster.Batch)},
		batcher:    &drainBatcher{segments: segments.Load},
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	require.NoError(t, s.UploadSegments(drainCtx))
	t.Logf("drain returned with %d of %d batches still waiting to upload", segments.Load(), batches)
	require.Zero(t, segments.Load(), "drain returned before the scheduled batches were uploaded")
}
