package cluster

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestTransferOutcome(t *testing.T) {
	for want, err := range map[string]error{
		"accepted":         nil,
		"table_overloaded": &wal.TableOverloadedError{Reason: "bytes"},
		"too_large":        &wal.SegmentTooLargeError{Limit: 1},
		"bad_request":      &ErrBadRequest{Msg: "bad"},
		"exists":           ErrSegmentExists,
		"locked":           ErrSegmentLocked,
		"peer_overloaded":  &PeerOverloadedError{RetryAfter: time.Second},
		"error":            errors.New("boom"),
	} {
		require.Equal(t, want, transferOutcome(err), want)
	}
}

func counted(c *prometheus.CounterVec, outcome string) float64 {
	m := &dto.Metric{}
	if err := c.WithLabelValues(outcome).Write(m); err != nil {
		panic(err)
	}
	return m.GetCounter().GetValue()
}

func observed(h prometheus.Histogram) (count uint64, sum float64) {
	m := &dto.Metric{}
	if err := h.Write(m); err != nil {
		panic(err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

// sendTransfer sends one batch of three segments through a replicator with opts to a peer answering status, and waits
// for it to be released.
func sendTransfer(t *testing.T, opts ReplicatorOpts, status int) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()
	opts.Partitioner = &fakePartitioner{owner: "node2", addr: server.URL}
	opts.Health = &fakeHealthChecker{healthy: true}
	opts.SegmentRemover = &fakeSegmentRemover{}
	opts.InsecureSkipVerify, opts.Hostname, opts.DisableGzip = true, "node1", true

	rep, err := NewReplicator(opts)
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, rep.Open(ctx))
	defer rep.Close()

	dir := t.TempDir()
	var segments []wal.SegmentInfo
	for _, v := range []string{"1", "2", "3"} {
		s, err := wal.NewSegment(dir, "DB_Table")
		require.NoError(t, err)
		_, err = s.Write(context.Background(), []byte(v))
		require.NoError(t, err)
		require.NoError(t, s.Close())
		segments = append(segments, wal.SegmentInfo{Prefix: "DB_Table", Path: s.Path(), Size: s.Size()})
	}
	batch := &Batch{Database: "DB", Table: "Table", Prefix: "DB_Table", Segments: segments, batcher: &fakeBatcher{}}
	rep.TransferQueue() <- batch
	require.Eventually(t, batch.IsReleased, 5*time.Second, 10*time.Millisecond)
}

// Each transfer is counted by its outcome, with the number of segments it carried.
func TestReplicatorCountsTransfers(t *testing.T) {
	for outcome, status := range map[string]int{"accepted": http.StatusAccepted, "locked": http.StatusLocked, "error": http.StatusInternalServerError} {
		t.Run(outcome, func(t *testing.T) {
			outcomes := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "outcomes"}, []string{"outcome"})
			segments := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "segments"})
			sendTransfer(t, ReplicatorOpts{TransferOutcomes: outcomes, TransferSegments: segments}, status)
			require.Equal(t, 1.0, counted(outcomes, outcome))
			count, sum := observed(segments)
			require.EqualValues(t, 1, count)
			require.Equal(t, 3.0, sum)
		})
	}
}

// A replicator without transfer metrics, such as the collector's, records none, so the ingestor's peer transfer
// metrics count only transfers between ingestors.
func TestReplicatorWithoutTransferMetrics(t *testing.T) {
	before := counted(metrics.IngestorPeerTransfers, "accepted")
	count, _ := observed(metrics.IngestorPeerTransferSegments)
	sendTransfer(t, ReplicatorOpts{}, http.StatusAccepted)
	require.Equal(t, before, counted(metrics.IngestorPeerTransfers, "accepted"))
	after, _ := observed(metrics.IngestorPeerTransferSegments)
	require.Equal(t, count, after)
}

// TransferOutcomeNames lists every outcome transferOutcome reports.
func TestTransferOutcomeNamesCoverEveryOutcome(t *testing.T) {
	for _, err := range []error{nil, &wal.TableOverloadedError{Reason: "bytes"}, &wal.SegmentTooLargeError{Limit: 1}, &ErrBadRequest{Msg: "bad"},
		ErrSegmentExists, ErrSegmentLocked, &PeerOverloadedError{RetryAfter: time.Second}, errors.New("boom")} {
		require.Contains(t, TransferOutcomeNames, transferOutcome(err))
	}
}
