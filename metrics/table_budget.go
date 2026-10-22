package metrics

import (
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"sync"
)

var TableQuotaRejectedRecords = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Name: "table_quota_rejected_records_total", Help: "Records intentionally shed at table backlog quotas."}, []string{"component", "kind", "reason"})
var TableQuotaRejectedBytes = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Name: "table_quota_rejected_bytes_total", Help: "Uncompressed encoded bytes intentionally shed at table backlog quotas."}, []string{"component", "kind", "reason"})

// IngestorUndeliverableSegments counts segments a destination can never accept
// because the payload exceeds its entire budget. This requires an operator to
// raise the table budget or lower the transfer size; the data is retained.
var IngestorUndeliverableSegments = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Subsystem: "ingestor", Name: "undeliverable_segments_total", Help: "Segments that exceed the destination table budget and cannot be split further."}, []string{"database", "table"})

// IngestorDeferredBatches counts batches left on disk because a destination
// already contributed its share to this dispatch round. They are retried on the
// next scan; a sustained rate means that destination is producing faster than
// the pipeline drains it.
var IngestorDeferredBatches = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Subsystem: "ingestor", Name: "deferred_batches_total", Help: "Batches deferred because a destination reached its per-round dispatch limit."}, []string{"database", "table"})

type tableBudgetCollector struct {
	mu           sync.Mutex
	sources      map[string]*wal.Admission
	descriptions []*prometheus.Desc
}

func newTableBudgetCollector() *tableBudgetCollector {
	c := &tableBudgetCollector{sources: make(map[string]*wal.Admission)}
	for _, name := range []string{"bytes", "segments", "limit_bytes", "limit_segments", "throttled", "throttled_seconds", "enforced", "would_reject", "rejected_records", "rejected_bytes", "oldest_age_seconds"} {
		c.descriptions = append(c.descriptions, prometheus.NewDesc("adxmon_table_backlog_"+name, "Current table backlog budget; retired when its last file drains.", []string{"component", "database", "table"}, nil))
	}
	return c
}

var tableBudgets = newTableBudgetCollector()

func init() { prometheus.MustRegister(tableBudgets) }

func RegisterTableBudget(component string, budget *wal.Admission) func() {
	tableBudgets.mu.Lock()
	tableBudgets.sources[component] = budget
	tableBudgets.mu.Unlock()
	return func() {
		tableBudgets.mu.Lock()
		defer tableBudgets.mu.Unlock()
		if tableBudgets.sources[component] == budget {
			delete(tableBudgets.sources, component)
		}
	}
}
func (c *tableBudgetCollector) Describe(out chan<- *prometheus.Desc) {
	for _, d := range c.descriptions {
		out <- d
	}
}
func (c *tableBudgetCollector) Collect(out chan<- prometheus.Metric) {
	c.mu.Lock()
	sources := make(map[string]*wal.Admission, len(c.sources))
	for name, b := range c.sources {
		sources[name] = b
	}
	c.mu.Unlock()
	for component, budget := range sources {
		for destination, s := range budget.Status() {
			throttled := float64(0)
			if s.Throttled {
				throttled = 1
			}
			enforced := float64(0)
			if s.Enforced {
				enforced = 1
			}
			values := []float64{float64(s.Bytes), float64(s.Segments), float64(s.Limit.Bytes), float64(s.Limit.Segments), throttled, s.ThrottledFor.Seconds(), enforced, float64(s.WouldReject), float64(s.RejectedRecords), float64(s.RejectedBytes), s.OldestAge.Seconds()}
			for n, value := range values {
				out <- prometheus.MustNewConstMetric(c.descriptions[n], prometheus.GaugeValue, value, component, destination.Database, destination.Table)
			}
		}
	}
}
