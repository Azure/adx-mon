package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestReplicator_PipelineMetrics(t *testing.T) {
	for _, component := range []string{"collector", "ingestor"} {
		for _, tt := range []struct {
			name    string
			status  int
			retries float64
			removed bool
		}{
			{name: "success", status: http.StatusAccepted, removed: true},
			{name: "rejected", status: http.StatusBadRequest, removed: true},
			{name: "already present", status: http.StatusConflict, removed: true},
			{name: "locked", status: http.StatusLocked, retries: 1},
			{name: "overloaded", status: http.StatusTooManyRequests, retries: 1},
			{name: "server error", status: http.StatusInternalServerError, retries: 1},
		} {
			t.Run(component+"/"+tt.name, func(t *testing.T) {
				metrics.PipelineStepDurationSeconds.Reset()
				metrics.PipelineRetriesScheduledTotal.Reset()
				t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
				t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					w.WriteHeader(tt.status)
				}))
				defer server.Close()
				rep, err := NewReplicator(ReplicatorOpts{
					Component:   component,
					Hostname:    "sender",
					Partitioner: &fakePartitioner{owner: "receiver", addr: server.URL},
					Health:      &fakeHealthChecker{healthy: true},
					DisableGzip: true,
				})
				require.NoError(t, err)
				r := rep.(*replicator)
				t.Cleanup(r.cli.httpClient.CloseIdleConnections)
				batch := newTransferMetricsBatch(t)

				r.transfer(context.Background(), batch)

				require.True(t, batch.IsReleased())
				require.Equal(t, tt.removed, batch.IsRemoved())
				require.Equal(t, tt.retries, pipelineRetriesScheduled(t, component))
				for _, result := range []string{"success", "error"} {
					var m dto.Metric
					h := metrics.PipelineStepDurationSeconds.WithLabelValues(component, "transfer", "DB", "Table", result)
					require.NoError(t, h.(prometheus.Metric).Write(&m))
					success := tt.status == http.StatusAccepted
					if (result == "success") == success {
						require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
						require.Positive(t, m.GetHistogram().GetSampleSum())
					} else {
						require.Zero(t, m.GetHistogram().GetSampleCount())
					}
				}
			})
		}
	}
}

func TestReplicator_PipelineMetricsSkippedTransfers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		owner   string
		healthy bool
	}{
		{name: "local owner", owner: "sender", healthy: true},
		{name: "unhealthy peer", owner: "receiver"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics.PipelineStepDurationSeconds.Reset()
			metrics.PipelineRetriesScheduledTotal.Reset()
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
			health := NewHealth(HealthOpts{UnhealthyTimeout: time.Hour})
			if !tt.healthy {
				health.SetPeerUnhealthy(tt.owner)
			}
			rep, err := NewReplicator(ReplicatorOpts{
				Hostname:    "sender",
				Partitioner: &fakePartitioner{owner: tt.owner},
				Health:      health,
			})
			require.NoError(t, err)
			r := rep.(*replicator)
			t.Cleanup(r.cli.httpClient.CloseIdleConnections)
			batch := newTransferMetricsBatch(t)

			r.transfer(context.Background(), batch)

			require.True(t, batch.IsReleased())
			require.False(t, batch.IsRemoved())
			registry := prometheus.NewRegistry()
			registry.MustRegister(metrics.PipelineStepDurationSeconds, metrics.PipelineRetriesScheduledTotal)
			families, err := registry.Gather()
			require.NoError(t, err)
			require.Empty(t, families)
		})
	}
}

func TestReplicator_PipelineMetricsContextErrors(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "timeout"
		if canceled {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			metrics.PipelineStepDurationSeconds.Reset()
			metrics.PipelineRetriesScheduledTotal.Reset()
			t.Cleanup(metrics.PipelineStepDurationSeconds.Reset)
			t.Cleanup(metrics.PipelineRetriesScheduledTotal.Reset)
			rep, err := NewReplicator(ReplicatorOpts{
				Hostname:    "sender",
				Partitioner: &fakePartitioner{owner: "receiver", addr: "http://localhost"},
				Health:      &fakeHealthChecker{healthy: true},
				DisableGzip: true,
			})
			require.NoError(t, err)
			r := rep.(*replicator)
			t.Cleanup(r.cli.httpClient.CloseIdleConnections)
			batch := newTransferMetricsBatch(t)
			var ctx context.Context
			var cancel context.CancelFunc
			if canceled {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			defer cancel()

			r.transfer(ctx, batch)

			require.True(t, batch.IsReleased())
			require.False(t, batch.IsRemoved())
			retries := pipelineRetriesScheduled(t, "ingestor")
			if canceled {
				require.Zero(t, retries)
			} else {
				require.Equal(t, float64(1), retries)
			}
			var m dto.Metric
			h := metrics.PipelineStepDurationSeconds.WithLabelValues("ingestor", "transfer", "DB", "Table", "error")
			require.NoError(t, h.(prometheus.Metric).Write(&m))
			require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
		})
	}
}

func TestReplicator_RejectsInvalidComponent(t *testing.T) {
	_, err := NewReplicator(ReplicatorOpts{Component: "unknown"})
	require.ErrorContains(t, err, "invalid replicator component")
}

func TestReplicator_LifecycleRetry(t *testing.T) {
	metrics.PipelineSegmentDurationSeconds.Reset()
	metrics.PipelineSegmentRetriesTotal.Reset()
	t.Cleanup(metrics.PipelineSegmentDurationSeconds.Reset)
	t.Cleanup(metrics.PipelineSegmentRetriesTotal.Reset)
	status := http.StatusLocked
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
	}))
	defer server.Close()
	rep, err := NewReplicator(ReplicatorOpts{
		Component: "ingestor", Hostname: "sender", DisableGzip: true,
		Partitioner: &fakePartitioner{owner: "receiver", addr: server.URL},
		Health:      &fakeHealthChecker{healthy: true},
	})
	require.NoError(t, err)
	r := rep.(*replicator)
	t.Cleanup(r.cli.httpClient.CloseIdleConnections)
	batch := newTransferMetricsBatch(t)
	batch.Database, batch.Table = "DB", "Table"
	now := time.Now()
	batch.Segments[0].Timing = wal.NewSegmentTiming(batch.Segments[0].CreatedAt, now, false)
	batch.queuedAt = now
	r.transfer(context.Background(), batch)
	require.False(t, batch.IsRemoved())
	// Rebatching copies SegmentInfo, including the shared timing history.
	second := &Batch{
		Database: batch.Database, Table: batch.Table, Segments: append([]wal.SegmentInfo(nil), batch.Segments...),
		batcher: &fakeBatcher{}, queuedAt: time.Now(),
	}
	status = http.StatusAccepted
	r.transfer(context.Background(), second)
	require.True(t, second.IsRemoved())
	var metric dto.Metric
	var sum float64
	for _, step := range []string{"wal_buffer", "batch_wait", "retry_wait", "worker_queue", "processing", "total"} {
		h := metrics.PipelineSegmentDurationSeconds.WithLabelValues("ingestor", step, "DB", "Table", "peer")
		require.NoError(t, h.(prometheus.Metric).Write(&metric))
		require.EqualValues(t, 1, metric.GetHistogram().GetSampleCount(), step)
		if step == "total" {
			require.InDelta(t, sum, metric.GetHistogram().GetSampleSum(), 1e-9)
		} else {
			sum += metric.GetHistogram().GetSampleSum()
		}
	}
	require.NoError(t, metrics.PipelineSegmentRetriesTotal.WithLabelValues("ingestor", "DB", "Table", "peer").Write(&metric))
	require.Equal(t, float64(1), metric.GetCounter().GetValue())
}

func newTransferMetricsBatch(t *testing.T) *Batch {
	t.Helper()
	seg, err := wal.NewSegment(t.TempDir(), "DB_Table")
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), []byte("test\n"))
	require.NoError(t, err)
	info := seg.Info()
	require.NoError(t, seg.Close())
	return &Batch{Segments: []wal.SegmentInfo{info}, batcher: &fakeBatcher{}}
}

func pipelineRetriesScheduled(t *testing.T, component string) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, metrics.PipelineRetriesScheduledTotal.WithLabelValues(component, "transfer", "DB", "Table").Write(&m))
	return m.GetCounter().GetValue()
}

func TestReplicator_SuccessfulTransfer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(time.Second)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_BadRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid file name"))
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_SegmentExistsError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.True(t, batch.IsRemoved())
}

func TestReplicator_PeerOverloadedError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())

}

func TestReplicator_UnknownError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	opts := ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	}

	rep, err := NewReplicator(opts)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = rep.Open(ctx)
	require.NoError(t, err)
	defer rep.Close()

	segmentPath := filepath.Join(os.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{
			{Path: segmentPath},
		},
		batcher: &fakeBatcher{},
	}

	rep.TransferQueue() <- batch
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, rep.Close())

	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())

}

func TestReplicator_TransfersRealtimeBatches(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:        &fakePartitioner{owner: "node2", addr: server.URL},
		Health:             &fakeHealthChecker{healthy: true},
		SegmentRemover:     &fakeSegmentRemover{},
		InsecureSkipVerify: true,
		Hostname:           "node1",
		DisableGzip:        true,
	})
	require.NoError(t, err)
	require.NotEqual(t, rep.TransferQueue(), rep.RealtimeTransferQueue())
	require.NoError(t, rep.Open(context.Background()))

	segmentPath := filepath.Join(t.TempDir(), "DB_Table_1.wal")
	require.NoError(t, os.WriteFile(segmentPath, []byte("test"), 0644))
	batch := &Batch{
		Segments: []wal.SegmentInfo{{Path: segmentPath}},
		batcher:  &fakeBatcher{},
	}

	rep.RealtimeTransferQueue() <- batch
	require.Eventually(t, batch.IsRemoved, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, rep.Close())
	require.True(t, batch.IsReleased())
}

func TestReplicator_SingleWorkerProcessesBothQueues(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:            &fakePartitioner{owner: "node2", addr: server.URL},
		Health:                 &fakeHealthChecker{healthy: true},
		SegmentRemover:         &fakeSegmentRemover{},
		InsecureSkipVerify:     true,
		Hostname:               "node1",
		DisableGzip:            true,
		MaxTransferConcurrency: 1,
	})
	require.NoError(t, err)
	require.NoError(t, rep.Open(context.Background()))

	dir := t.TempDir()
	newBatch := func(name string) *Batch {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("test"), 0644))
		return &Batch{Segments: []wal.SegmentInfo{{Path: path}}, batcher: &fakeBatcher{}}
	}
	queued, realtime := newBatch("DB_Queued_1.wal"), newBatch("DB_Realtime_1.wal")
	rep.TransferQueue() <- queued
	rep.RealtimeTransferQueue() <- realtime

	require.Eventually(t, func() bool { return queued.IsRemoved() && realtime.IsRemoved() }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, rep.Close())
}
