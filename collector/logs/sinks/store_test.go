package sinks

import (
	"context"
	"errors"
	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/storage"
	"github.com/stretchr/testify/require"
	"testing"
)

type outcomeStore struct {
	storage.Store
	err error
}

func (s outcomeStore) WriteNativeLogs(context.Context, *types.LogBatch) error { return s.err }

func TestStoreSinkAcknowledgesSheddingButNotFailures(t *testing.T) {
	for _, err := range []error{nil, &storage.WriteOutcome{Accepted: 1, Rejected: 1}, errors.New("disk failure")} {
		acked := false
		batch := &types.LogBatch{Ack: func() { acked = true }}
		s, e := NewStoreSink(StoreSinkConfig{Store: outcomeStore{err: err}})
		require.NoError(t, e)
		require.Equal(t, err, s.Send(context.Background(), batch))
		_, failure := storage.SplitOutcome(err)
		require.Equal(t, failure == nil, acked)
	}
}
