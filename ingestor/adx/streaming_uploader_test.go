// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	adxschema "github.com/Azure/adx-mon/schema"
	azkustoingest "github.com/Azure/azure-kusto-go/azkustoingest"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

type fakeStreamer struct {
	mu    sync.Mutex
	err   error
	calls int
	data  []byte
}

func (f *fakeStreamer) FromReader(ctx context.Context, reader io.Reader, options ...azkustoingest.FileOption) (*azkustoingest.Result, error) {
	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.data = b
	return nil, f.err
}

func (f *fakeStreamer) Close() error { return nil }

type fakeQueued struct {
	calls int
	data  []byte
	err   error
}

func (f *fakeQueued) upload(reader io.Reader, database, table string, mapping adxschema.SchemaMapping) error {
	b, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	f.calls++
	f.data = b
	return f.err
}

type localPartitioner struct{}

func (localPartitioner) Owner([]byte) (string, string) { return "local", "" }

type healthyPeers struct{}

func (healthyPeers) IsPeerHealthy(string) bool { return true }
func (healthyPeers) SetPeerUnhealthy(string)   {}
func (healthyPeers) SetPeerHealthy(string)     {}

type streamingTestEnv struct {
	dir      string
	idx      *wal.Index
	batcher  cluster.Batcher
	realtime chan *cluster.Batch
	queued   chan *cluster.Batch
	u        *uploader
	stream   *fakeStreamer
	queue    *fakeQueued
	mgmt     *countingKustoMgmt
}

func newStreamingTestEnv(t *testing.T) *streamingTestEnv {
	t.Helper()
	env := &streamingTestEnv{
		dir:      t.TempDir(),
		idx:      wal.NewIndex(),
		realtime: make(chan *cluster.Batch, 10),
		queued:   make(chan *cluster.Batch, 10),
		stream:   &fakeStreamer{},
		queue:    &fakeQueued{},
		mgmt:     &countingKustoMgmt{},
	}

	b, err := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir:              env.dir,
		MaxTransferAge:          time.Hour,
		Partitioner:             localPartitioner{},
		Segmenter:               env.idx,
		UploadQueue:             env.queued,
		TransferQueue:           env.queued,
		RealtimeUploadQueue:     env.realtime,
		PeerHealthReporter:      healthyPeers{},
		SegmentsCountMetric:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "count"}),
		SegmentsSizeBytesMetric: prometheus.NewGauge(prometheus.GaugeOpts{Name: "size"}),
		SegmentsMaxAgeMetric:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "age"}),
	})
	require.NoError(t, err)
	env.batcher = b

	env.u = NewUploader(nil, UploaderOpts{
		Database:       "db",
		DefaultMapping: adxschema.DefaultMetricsMapping,
		Realtime: &RealtimeUploadOpts{
			Slots:  NewStreamingSlots(4, 1, 0, cluster.PeerInfo{Count: 1}),
			MaxLag: time.Minute,
		},
	})
	env.u.syncer = NewSyncer(env.mgmt, "db", adxschema.DefaultMetricsMapping, PromMetrics)
	env.u.streamer = env.stream
	env.u.queuedUpload = env.queue.upload
	return env
}

// addSegment writes a segment with data for table and returns its path.
func (e *streamingTestEnv) addSegment(t *testing.T, table string, priority ingestpolicy.Priority, data []byte, createdAt time.Time) string {
	t.Helper()
	seg, err := wal.NewSegment(e.dir, "db_"+table)
	require.NoError(t, err)
	_, err = seg.Write(context.Background(), data)
	require.NoError(t, err)
	info := seg.Info()
	require.NoError(t, seg.Close())

	info.Size = int64(len(data))
	info.Priority = priority
	if !createdAt.IsZero() {
		info.CreatedAt = createdAt
	}
	e.idx.Add(info)
	return info.Path
}

// nextBatch batches the indexed segments and returns the next batch for priority.
func (e *streamingTestEnv) nextBatch(t *testing.T, priority ingestpolicy.Priority) *cluster.Batch {
	t.Helper()
	require.NoError(t, e.batcher.BatchSegments())
	ch := e.queued
	if priority == ingestpolicy.PriorityRealtime {
		ch = e.realtime
	}
	select {
	case b := <-ch:
		return b
	default:
		t.Fatal("no batch")
		return nil
	}
}

var testCSV = []byte("2024-01-01T00:00:00Z,1,{},1.5\n")

func TestStreamBatch_Success(t *testing.T) {
	env := newStreamingTestEnv(t)
	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

	require.Equal(t, 1, env.stream.calls)
	require.Equal(t, testCSV, env.stream.data)
	require.Zero(t, env.queue.calls)
	require.NoFileExists(t, path)
	require.Zero(t, env.idx.TotalSegments())
	require.Zero(t, env.u.realtime.Slots.Stats().InUse)

	// The table, mapping and streaming policy are ensured before streaming.
	require.Contains(t, env.mgmt.queries, ".alter table Cpu policy streamingingestion enable")
}

func TestUploader_CloseWaitsForStreamingUpload(t *testing.T) {
	env := newStreamingTestEnv(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	env.u.streamer = blockingStreamer{started: started, finish: finish}
	env.u.realtimeQueue = env.realtime
	env.u.queue = env.queued
	env.u.syncer.cancelFn = func() {}
	ctx, cancel := context.WithCancel(context.Background())
	env.u.ctx, env.u.closeFn = ctx, cancel
	env.u.opts.ConcurrentUploads = 1
	env.u.startWorkers(ctx)

	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
	require.NoError(t, env.batcher.BatchSegments())
	<-started

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = env.u.Close()
	}()
	select {
	case <-closed:
		t.Fatal("Close returned with streaming upload still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(finish)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after streaming upload finished")
	}
}

type blockingStreamer struct {
	started chan struct{}
	finish  chan struct{}
}

func (s blockingStreamer) FromReader(context.Context, io.Reader, ...azkustoingest.FileOption) (*azkustoingest.Result, error) {
	close(s.started)
	<-s.finish
	return nil, nil
}

func (blockingStreamer) Close() error { return nil }

func TestStreamBatch_QueuedBatchesUseQueuedIngestion(t *testing.T) {
	env := newStreamingTestEnv(t)
	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityQueued, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityQueued))

	require.Zero(t, env.stream.calls)
	require.Equal(t, 1, env.queue.calls)
	require.Equal(t, testCSV, env.queue.data)
	require.NoFileExists(t, path)
}

func TestStreamBatch_NoStreamerUsesQueuedIngestion(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.streamer = nil
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

	require.Equal(t, 1, env.queue.calls)
	require.Equal(t, testCSV, env.queue.data)
}

func TestStreamBatch_RetryableFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		throttled bool
	}{
		{name: "throttled", err: newStreamingHTTPError("429 Too Many Requests", 429, ""), throttled: true},
		{name: "server error", err: newStreamingHTTPError("500 Internal Server Error", 500, ""), throttled: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newStreamingTestEnv(t)
			env.stream.err = tt.err
			path := env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

			batch := env.nextBatch(t, ingestpolicy.PriorityRealtime)
			env.u.uploadBatch(batch)

			// The batch is released to be retried later and its segment is kept.
			require.Equal(t, 1, env.stream.calls)
			require.Zero(t, env.queue.calls)
			require.FileExists(t, path)
			require.True(t, batch.IsReleased())
			require.False(t, batch.IsRemoved())

			stats := env.u.realtime.Slots.Stats()
			require.Zero(t, stats.InUse)
			if tt.throttled {
				require.Equal(t, 2, stats.Limit)
			} else {
				require.Equal(t, 4, stats.Limit)
			}
		})
	}
}

func TestStreamBatch_FallbackFailures(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		cooldown bool
	}{
		{name: "too large", err: newStreamingHTTPError("413 Request Entity Too Large", 413, "")},
		{name: "permanent", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("Stream_WrongNumberOfFields", "bad"))},
		{name: "unavailable", err: newStreamingHTTPError("400 Bad Request", 400, kustoBody("BadRequest_EntityNotFound", "missing")), cooldown: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newStreamingTestEnv(t)
			env.stream.err = tt.err
			path := env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

			env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

			// The buffered data is ingested with queued ingestion.
			require.Equal(t, 1, env.stream.calls)
			require.Equal(t, 1, env.queue.calls)
			require.Equal(t, testCSV, env.queue.data)
			require.NoFileExists(t, path)
			require.Equal(t, tt.cooldown, env.u.cooldowns.active("Cpu", time.Now()))

			// During a cooldown the table skips streaming.
			env.stream.err = nil
			env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
			env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))
			if tt.cooldown {
				require.Equal(t, 1, env.stream.calls)
				require.Equal(t, 2, env.queue.calls)
			} else {
				require.Equal(t, 2, env.stream.calls)
				require.Equal(t, 1, env.queue.calls)
			}
		})
	}
}

func TestStreamBatch_CooldownExpires(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.cooldowns.start("Cpu", time.Now().Add(-time.Second))
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))
	require.Equal(t, 1, env.stream.calls)
	require.Zero(t, env.queue.calls)
}

func TestStreamBatch_LaggingBatchUsesQueuedIngestion(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Now().Add(-2*time.Minute))

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

	require.Zero(t, env.stream.calls)
	require.Equal(t, 1, env.queue.calls)
	require.Equal(t, testCSV, env.queue.data)
}

func TestStreamBatch_SlotWaitPastLagUsesQueuedIngestion(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.realtime.MaxLag = 50 * time.Millisecond
	env.u.realtime.Slots = NewStreamingSlots(1, 1, 0, cluster.PeerInfo{Count: 1})
	release, err := env.u.realtime.Slots.Acquire(context.Background())
	require.NoError(t, err)
	defer release(false)

	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
	start := time.Now()
	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

	require.Less(t, time.Since(start), time.Second)
	require.Zero(t, env.stream.calls)
	require.Equal(t, 1, env.queue.calls)
	require.Equal(t, testCSV, env.queue.data)
}

func TestStreamBatch_TooLargeRequestUsesQueuedIngestion(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.realtime.MaxRequestBytes = 64

	var data []byte
	for i := 0; i < 10; i++ {
		data = append(data, []byte(fmt.Sprintf("2024-01-01T00:00:0%dZ,%d,{},1.5\n", i, i))...)
	}
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, data, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

	// The partially read data and the remainder are both ingested.
	require.Zero(t, env.stream.calls)
	require.Equal(t, 1, env.queue.calls)
	require.Equal(t, data, env.queue.data)
}

func TestStreamBatch_DirectIngestUsesQueuedPath(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.requireDirectIngest = true
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))
	require.Zero(t, env.stream.calls)
	require.Equal(t, 1, env.queue.calls)
}

func TestStreamBatch_StreamingPolicyFailureStillStreams(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.syncer.streamingPolicies["Cpu"] = streamingPolicyState{err: errors.New("forbidden"), retryAt: time.Now().Add(time.Hour)}
	env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))
	require.Equal(t, 1, env.stream.calls)
	require.Zero(t, env.queue.calls)
}

func TestStreamBatch_MultipleSegments(t *testing.T) {
	env := newStreamingTestEnv(t)
	var paths []string
	var want bytes.Buffer
	for i := 0; i < 3; i++ {
		line := []byte(fmt.Sprintf("2024-01-01T00:00:0%dZ,%d,{},1.5\n", i, i))
		want.Write(line)
		paths = append(paths, env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, line, time.Time{}))
	}

	env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))
	require.Equal(t, want.Bytes(), env.stream.data)
	for _, p := range paths {
		require.NoFileExists(t, p)
	}
	_, err := os.Stat(env.dir)
	require.NoError(t, err)
}

func TestRealtimeUploadOptsDefaults(t *testing.T) {
	opts := (&RealtimeUploadOpts{MaxRequestBytes: 10 * maxStreamingRequestBytes}).withDefaults()
	require.Equal(t, maxStreamingRequestBytes, opts.MaxRequestBytes)
	require.Equal(t, defaultStreamingCooldown, opts.Cooldown)
	require.Equal(t, defaultStreamingRequestTimeout, opts.RequestTimeout)

	opts = (&RealtimeUploadOpts{MaxRequestBytes: 100, Cooldown: time.Second, RequestTimeout: time.Second}).withDefaults()
	require.Equal(t, int64(100), opts.MaxRequestBytes)
	require.Equal(t, time.Second, opts.Cooldown)
	require.Equal(t, time.Second, opts.RequestTimeout)
}

func TestStreamBatch_NoSlotRetriesWithoutHoldingWorker(t *testing.T) {
	env := newStreamingTestEnv(t)
	env.u.realtime.Slots = NewStreamingSlots(1, 1, 0, cluster.PeerInfo{Count: 1})
	release, err := env.u.realtime.Slots.Acquire(context.Background())
	require.NoError(t, err)
	defer release(false)

	path := env.addSegment(t, "Cpu", ingestpolicy.PriorityRealtime, testCSV, time.Time{})
	batch := env.nextBatch(t, ingestpolicy.PriorityRealtime)

	start := time.Now()
	env.u.uploadBatch(batch)

	// The worker gives up after a short wait and the batch is retried later rather than using queued ingestion.
	require.Less(t, time.Since(start), maxSlotWait+500*time.Millisecond)
	require.GreaterOrEqual(t, time.Since(start), maxSlotWait)
	require.Zero(t, env.stream.calls)
	require.Zero(t, env.queue.calls)
	require.FileExists(t, path)
	require.True(t, batch.IsReleased())
	require.False(t, batch.IsRemoved())
}

func realtimeBatchCount(t *testing.T, table, outcome string) float64 {
	t.Helper()
	m := &dto.Metric{}
	require.NoError(t, metrics.IngestorRealtimeBatches.WithLabelValues("db", table, outcome).Write(m))
	return m.GetCounter().GetValue()
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	m := &dto.Metric{}
	require.NoError(t, c.Write(m))
	return m.GetCounter().GetValue()
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	m := &dto.Metric{}
	require.NoError(t, g.Write(m))
	return m.GetGauge().GetValue()
}

func TestStreamBatch_Metrics(t *testing.T) {
	for _, tt := range []struct {
		name      string
		setup     func(env *streamingTestEnv)
		createdAt time.Time
		outcome   string
		streamed  bool
	}{
		{name: "streamed", outcome: "streamed", streamed: true},
		{name: "throttled", setup: func(env *streamingTestEnv) { env.stream.err = newStreamingHTTPError("429 Too Many Requests", 429, "") }, outcome: "retry_throttled"},
		{name: "transient", setup: func(env *streamingTestEnv) {
			env.stream.err = newStreamingHTTPError("500 Internal Server Error", 500, "")
		}, outcome: "retry_transient"},
		{name: "unavailable", setup: func(env *streamingTestEnv) {
			env.stream.err = newStreamingHTTPError("404 Not Found", 404, "")
		}, outcome: "fallback_unavailable"},
		{name: "lag", createdAt: time.Now().Add(-time.Hour), outcome: "fallback_lag"},
		{name: "direct ingest", setup: func(env *streamingTestEnv) { env.u.requireDirectIngest = true }, outcome: "fallback_direct_ingest"},
		{name: "no slot", setup: func(env *streamingTestEnv) {
			env.u.realtime.Slots = NewStreamingSlots(1, 1, 0, cluster.PeerInfo{Count: 1})
			_, ok := env.u.realtime.Slots.TryAcquire()
			require.True(t, ok)
		}, outcome: "retry_no_slot"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newStreamingTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			table := "Metrics" + strings.ReplaceAll(tt.name, " ", "")
			env.addSegment(t, table, ingestpolicy.PriorityRealtime, testCSV, tt.createdAt)

			before := realtimeBatchCount(t, table, tt.outcome)
			requests := counterValue(t, metrics.IngestorRealtimeStreamingRequests.WithLabelValues("db"))
			duration := counterValue(t, metrics.IngestorRealtimeStreamingDuration.WithLabelValues("db"))
			env.u.uploadBatch(env.nextBatch(t, ingestpolicy.PriorityRealtime))

			require.Equal(t, before+1, realtimeBatchCount(t, table, tt.outcome))

			// Requests and their duration are counted whenever a streaming request is sent.
			sent := env.stream.calls > 0
			gotRequests := counterValue(t, metrics.IngestorRealtimeStreamingRequests.WithLabelValues("db"))
			gotDuration := counterValue(t, metrics.IngestorRealtimeStreamingDuration.WithLabelValues("db"))
			if sent {
				require.Equal(t, requests+1, gotRequests)
				require.Greater(t, gotDuration, duration)
			} else {
				require.Equal(t, requests, gotRequests)
				require.Equal(t, duration, gotDuration)
			}

			latency := gaugeValue(t, metrics.IngestorRealtimeIngestLatency.WithLabelValues("db", table))
			if tt.streamed {
				require.Greater(t, latency, float64(0))
			} else {
				require.Zero(t, latency)
			}
		})
	}
}
