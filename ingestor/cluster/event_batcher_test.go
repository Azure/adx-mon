// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/ingestpolicy"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/stretchr/testify/require"
)

// transferPolicy batches queued segments into a single batch that is transferred to a peer.
type transferPolicy struct{}

func (transferPolicy) accepts(si wal.SegmentInfo) bool {
	return si.Priority == ingestpolicy.PriorityQueued
}
func (transferPolicy) maxLatency(string) time.Duration { return time.Hour }
func (transferPolicy) sizeTrigger() int64              { return 1 << 40 }

func (transferPolicy) maxBatches() int { return 0 }

func (transferPolicy) batches(b *batcher, prefix string, segments []wal.SegmentInfo, _ int) (owned, notOwned []*Batch) {
	batch := &Batch{Prefix: prefix, Priority: ingestpolicy.PriorityQueued, batcher: b}
	for _, si := range segments {
		batch.Segments = append(batch.Segments, si)
		_ = b.segments.Mutate(si.Path, func(n int) (int, error) { return n + 1, nil })
	}
	return nil, []*Batch{batch}
}

func TestEventBatcher_SendsNotOwnedToTransferQueue(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	e := newEventBatcher(env.b, transferPolicy{}, time.Hour)

	q := env.add(t, "Queued", 100, ingestpolicy.PriorityQueued)
	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	e.Flush(context.Background())

	// Only accepted segments are batched, and notOwned batches go to the transfer queue for their priority.
	require.Len(t, env.transfer, 1)
	require.Equal(t, []string{q.Path}, (<-env.transfer).Paths())
	require.Empty(t, env.upload)
	require.Empty(t, env.realtime)
}

func TestEventBatcher_ReleasesUnsentNotOwnedOnCancel(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	env.b.transferQueue = make(chan *Batch) // Unbuffered with no reader.
	e := newEventBatcher(env.b, transferPolicy{}, time.Hour)

	q := env.add(t, "Queued", 100, ingestpolicy.PriorityQueued)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Flush(ctx)

	n, _ := env.b.segments.Get(q.Path)
	require.Zero(t, n)
}

func TestEventBatcher_SweepSkipsBatchedPrefixes(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	e := newEventBatcher(env.b, transferPolicy{}, time.Hour)

	inFlight := env.add(t, "InFlight", 100, ingestpolicy.PriorityQueued)
	_ = env.b.segments.Mutate(inFlight.Path, func(n int) (int, error) { return n + 1, nil })
	env.add(t, "Partial", 100, ingestpolicy.PriorityQueued)
	partial := env.add(t, "Partial", 100, ingestpolicy.PriorityQueued)
	_ = env.b.segments.Mutate(partial.Path, func(n int) (int, error) { return n + 1, nil })
	env.add(t, "Realtime", 100, ingestpolicy.PriorityRealtime)

	e.sweep()

	// Only prefixes with unbatched segments accepted by the policy are pending.
	_, inFlightPending := e.pending["db_InFlight"]
	_, partialPending := e.pending["db_Partial"]
	_, realtimePending := e.pending["db_Realtime"]
	require.False(t, inFlightPending)
	require.True(t, partialPending)
	require.False(t, realtimePending)
	require.Len(t, e.pending, 1)
}

func TestEventBatcher_SweepPrunesStalePrefixEntries(t *testing.T) {
	env := newRealtimeTestEnv(t, RealtimeBatchOpts{}, false)
	e := newEventBatcher(env.b, transferPolicy{}, time.Hour)

	// A live prefix to prove the prune is scoped to the live set, not unconditional.
	live := env.add(t, "Live", 100, ingestpolicy.PriorityQueued)

	// Seed stale entries for a prefix in the maps that track retry/backoff state.
	const prefix = "db_Gone"
	e.mu.Lock()
	e.releases[prefix] = 3
	e.failed[prefix] = true
	e.backlog[prefix] = true
	e.outstanding[prefix] = 2
	e.pending[prefix] = pendingPrefix{deadline: time.Now().Add(time.Hour), seq: e.seq + 1}
	e.mu.Unlock()

	// Remove the prefix's segments from the index so it is no longer live, as after a peer transfer, audit, or manual
	// removal.  The state maps are only cleared on specific delivery/removal paths, so the entries linger until the
	// sweep prunes them.
	for i := 0; i < 2; i++ {
		env.idx.Remove(env.add(t, "Gone", 100, ingestpolicy.PriorityQueued))
	}

	e.sweep()

	e.mu.Lock()
	defer e.mu.Unlock()
	// The stale entries are pruned, but pending is left alone since it is cleaned separately.
	_, release := e.releases[prefix]
	_, failed := e.failed[prefix]
	_, backlog := e.backlog[prefix]
	_, outstanding := e.outstanding[prefix]
	_, pending := e.pending[prefix]
	require.False(t, release, "stale releases entry was not pruned")
	require.False(t, failed, "stale failed entry was not pruned")
	require.False(t, backlog, "stale backlog entry was not pruned")
	require.False(t, outstanding, "stale outstanding entry was not pruned")
	require.True(t, pending, "pending entries are managed separately and must survive the prune")
	// The live prefix is untouched by the prune.
	_, livePending := e.pending[live.Prefix]
	require.True(t, livePending, "a live prefix with unbatched segments must be marked pending by the sweep")
}
