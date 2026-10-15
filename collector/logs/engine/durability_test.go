package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
)

type durableSink struct {
	mockSink
	err error
}

func (s *durableSink) Durable() bool { return true }
func (s *durableSink) Send(ctx context.Context, batch *types.LogBatch) error {
	if s.err != nil {
		return s.err
	}
	return s.mockSink.Send(ctx, batch)
}

func newBatch() *types.LogBatch {
	return newBatchOf(1)
}

func newBatchOf(count int) *types.LogBatch {
	batch := types.LogBatchPool.Get(1).(*types.LogBatch)
	batch.Reset()
	for n := 0; n < count; n++ {
		log := types.LogPool.Get(1).(*types.Log)
		log.Reset()
		log.SetBodyValue("message", "value")
		batch.Logs = append(batch.Logs, log)
	}
	return batch
}

// A failing optional exporter must not make the producer resend records the
// durable sink already stored.
func TestOptionalExporterFailureDoesNotFailTheBatch(t *testing.T) {
	store := &durableSink{mockSink: mockSink{name: "store"}}
	exporter := &mockSink{name: "exporter", sendErr: errors.New("exporter unavailable")}
	w := &worker{SourceName: "test", Sinks: []types.Sink{store, exporter}, BatchTimeout: time.Second}

	rejected, err := w.ProcessBatch(context.Background(), newBatch())

	require.NoError(t, err, "optional exporter failure is not a producer-visible failure")
	require.Zero(t, rejected)
	require.Equal(t, 1, store.NumSends())
}

// A failing durable sink must still fail the batch so the producer retries.
func TestDurableSinkFailureFailsTheBatch(t *testing.T) {
	store := &durableSink{mockSink: mockSink{name: "store"}, err: errors.New("disk failure")}
	exporter := &mockSink{name: "exporter"}
	w := &worker{SourceName: "test", Sinks: []types.Sink{store, exporter}, BatchTimeout: time.Second}

	_, err := w.ProcessBatch(context.Background(), newBatch())

	require.Error(t, err)
}

// Shedding reported by the durable sink is surfaced without being treated as a
// failure.
func TestDurableSinkSheddingIsReported(t *testing.T) {
	store := &durableSink{mockSink: mockSink{name: "store"}, err: &storage.WriteOutcome{Accepted: 1, Rejected: 3}}
	w := &worker{SourceName: "test", Sinks: []types.Sink{store}, BatchTimeout: time.Second}

	rejected, err := w.ProcessBatch(context.Background(), newBatchOf(4))

	require.NoError(t, err)
	require.EqualValues(t, 3, rejected)
}

// With no durable sink every sink is authoritative, so data is never reported
// as stored when it went nowhere.
func TestWithoutDurableSinkAllFailuresCount(t *testing.T) {
	exporter := &mockSink{name: "exporter", sendErr: errors.New("exporter unavailable")}
	w := &worker{SourceName: "test", Sinks: []types.Sink{exporter}, BatchTimeout: time.Second}

	_, err := w.ProcessBatch(context.Background(), newBatch())

	require.Error(t, err)
}
