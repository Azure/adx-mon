package sinks

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/storage"
)

type StoreSinkConfig struct {
	Store        storage.Store
	MonitoredSet func(db, table string) bool // optional predicate, nil if monitoring disabled
}

type StoreSink struct {
	store        storage.Store
	monitoredSet func(db, table string) bool
}

func NewStoreSink(config StoreSinkConfig) (*StoreSink, error) {
	return &StoreSink{
		store:        config.Store,
		monitoredSet: config.MonitoredSet,
	}, nil
}

func (s *StoreSink) Open(ctx context.Context) error {
	return nil
}

func (s *StoreSink) Send(ctx context.Context, batch *types.LogBatch) error {
	// Count prior to attempting persistence so we can (intentionally) risk a slight
	// overcount if the subsequent write fails. This keeps the counting path off
	// the critical section that would otherwise require a second traversal or
	// per-log metric increments after success.
	countAndRecordMonitoredLogs(batch.Logs, s.monitoredSet)

	err := s.store.WriteNativeLogs(ctx, batch)
	if err != nil {
		return err
	}
	if batch.Ack != nil {
		batch.Ack()
	}
	return nil
}

func (s *StoreSink) Close() error {
	return nil
}

func (s *StoreSink) Name() string {
	return "StoreSink"
}

// countAndRecordMonitoredLogs aggregates counts per (db, table) for logs in the batch
// that satisfy the provided monitored predicate, and emits a single metric increment
// per pair.
//
// NOTE: This function is intended to be invoked BEFORE the storage write so that
// counting does not add latency after persistence. If the write subsequently fails
// an overcount will occur (accepted trade-off for Phase 0 visibility use‑case).
//
// Allocation: the counts map is only allocated lazily if at least one monitored
// log is encountered (zero allocations for unmonitored batches).
func countAndRecordMonitoredLogs(logs []*types.Log, monitoredSet func(db, table string) bool) {
	if monitoredSet == nil {
		return
	}
	var counts map[[2]string]int // lazy alloc
	for _, log := range logs {
		db := types.StringOrEmpty(log.GetAttributeValue(types.AttributeDatabaseName))
		if db == "" {
			continue
		}
		tbl := types.StringOrEmpty(log.GetAttributeValue(types.AttributeTableName))
		if tbl == "" {
			continue
		}
		if !monitoredSet(db, tbl) {
			continue
		}
		if counts == nil {
			counts = make(map[[2]string]int)
		}
		counts[[2]string{db, tbl}]++
	}
	for k, v := range counts {
		metrics.MonitoredLogsCollectedTotal.WithLabelValues(k[0], k[1]).Add(float64(v))
	}
}
