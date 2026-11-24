// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/schema"
	"github.com/Azure/azure-kusto-go/azkustoingest"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type burstStreamer struct {
	delivered chan string
}

func (s burstStreamer) FromReader(ctx context.Context, data io.Reader, _ ...azkustoingest.FileOption) (*azkustoingest.Result, error) {
	row, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(380 * time.Millisecond):
		s.delivered <- string(row)
		return nil, nil
	}
}

func (burstStreamer) Close() error { return nil }

// TestStreamingSlotBurst replays the same WAL burst on different revisions. Only the ADX clients are fakes.
// Run with INTEGRATION=1 go test -v ./ingestor/adx -run '^TestStreamingSlotBurst$' -count=1.
func TestStreamingSlotBurst(t *testing.T) {
	testutils.IntegrationTest(t)
	const batches = 60
	env := newStreamingTestEnv(t)
	u := env.u
	u.opts.ConcurrentUploads = 50
	u.realtime.Slots = NewStreamingSlots(1, 1, 0, cluster.PeerInfo{Count: 1})
	u.realtime.MaxLag = 30 * time.Second
	u.syncer.cancelFn = func() {}
	delivered := make(chan string, batches*2)
	u.streamer = burstStreamer{delivered}
	u.queuedUpload = func(_ context.Context, data io.Reader, _, _ string, _ schema.SchemaMapping) error {
		row, err := io.ReadAll(data)
		if err == nil {
			delivered <- string(row)
		}
		return err
	}

	batcher, err := cluster.NewBatcher(cluster.BatcherOpts{
		StorageDir: env.dir, Partitioner: localPartitioner{}, Segmenter: env.idx,
		PeerHealthReporter: healthyPeers{}, MaxTransferAge: time.Hour,
		UploadQueue: u.UploadQueue(), RealtimeUploadQueue: u.RealtimeUploadQueue(), TransferQueue: env.queued,
		MaxBatchSegments: 1, Realtime: cluster.RealtimeBatchOpts{MaxBatchBytes: 512 * 1024, MaxBatchLatency: 250 * time.Millisecond},
		SegmentsCountMetric:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "count"}),
		SegmentsSizeBytesMetric: prometheus.NewGauge(prometheus.GaugeOpts{Name: "size"}),
		SegmentsMaxAgeMetric:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "age"}),
	})
	require.NoError(t, err)
	const table = "SlotBurst"
	want := make([]string, batches)
	start := time.Now()
	for i := range want {
		want[i] = fmt.Sprintf("2024-01-01T00:00:00Z,%d,{},1.5\n", i)
		env.addSegment(t, table, ingestpolicy.PriorityRealtime, []byte(want[i]), start)
	}
	outcomes := []string{"streamed", "retry_no_slot", "fallback_lag"}
	before := make([]float64, len(outcomes))
	for i, outcome := range outcomes {
		before[i] = realtimeBatchCount(t, table, outcome)
	}
	u.startWorkers(t.Context())
	t.Cleanup(func() { require.NoError(t, u.Close()) })
	require.NoError(t, batcher.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, batcher.Close()) })
	require.Eventually(t, func() bool { return env.idx.TotalSegments() == 0 }, time.Minute, 10*time.Millisecond)
	elapsed := time.Since(start)
	close(delivered)
	var got []string
	for row := range delivered {
		got = append(got, row)
	}
	require.ElementsMatch(t, want, got)
	for i, outcome := range outcomes {
		t.Logf("%s=%.0f", outcome, realtimeBatchCount(t, table, outcome)-before[i])
	}
	t.Logf("drain=%s delivered=%d", elapsed.Round(time.Millisecond), len(got))
}
