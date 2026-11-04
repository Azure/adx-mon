package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// A successful transfer records the age of the batch's oldest segment.
func TestReplicatorRecordsTransferLatency(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer s.Close()

	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "transfer_latency_seconds"}, []string{"database"})
	rep, err := NewReplicator(ReplicatorOpts{
		Partitioner:     &fakePartitioner{owner: "remote", addr: s.URL},
		Health:          NewHealth(HealthOpts{}),
		Hostname:        "local",
		DisableGzip:     true,
		TransferLatency: latency,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	dir := t.TempDir()
	var segments []wal.SegmentInfo
	for _, age := range []time.Duration{30 * time.Second, 10 * time.Second} {
		seg, err := wal.NewSegment(dir, "DB_Table")
		require.NoError(t, err)
		_, err = seg.Write(ctx, []byte("payload"))
		require.NoError(t, err)
		require.NoError(t, seg.Close())
		info := seg.Info()
		info.CreatedAt = time.Now().Add(-age)
		segments = append(segments, info)
	}
	batch := &Batch{Database: "DB", Table: "Table", Segments: segments, batcher: &fakeBatcher{}}
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsRemoved, 5*time.Second, time.Millisecond)

	m := &dto.Metric{}
	require.NoError(t, latency.WithLabelValues("DB").(prometheus.Metric).Write(m))
	require.EqualValues(t, 1, m.GetHistogram().GetSampleCount())
	require.InDelta(t, 30, m.GetHistogram().GetSampleSum(), 1)
}
