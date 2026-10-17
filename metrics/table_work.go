package metrics

import (
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/prometheus/client_golang/prometheus"
)

type TableWork struct {
	Queued, Running int
	RetryAt         time.Time
}
type tableWorkCollector struct {
	mu           sync.Mutex
	sources      map[string]*tableWorkSource
	descriptions []*prometheus.Desc
}

type tableWorkSource struct {
	read func() map[wal.Destination]TableWork
}

var tableWorkMetrics = &tableWorkCollector{sources: make(map[string]*tableWorkSource)}

func init() {
	for _, name := range []string{"queued_batches", "running_batches", "retry_delay_seconds"} {
		tableWorkMetrics.descriptions = append(tableWorkMetrics.descriptions, prometheus.NewDesc("adxmon_table_work_"+name, "Current table work scheduling state.", []string{"pipeline", "database", "table"}, nil))
	}
	prometheus.MustRegister(tableWorkMetrics)
}
func RegisterTableWork(pipeline string, read func() map[wal.Destination]TableWork) func() {
	tableWorkMetrics.mu.Lock()
	source := &tableWorkSource{read: read}
	tableWorkMetrics.sources[pipeline] = source
	tableWorkMetrics.mu.Unlock()
	return func() {
		tableWorkMetrics.mu.Lock()
		if tableWorkMetrics.sources[pipeline] == source {
			delete(tableWorkMetrics.sources, pipeline)
		}
		tableWorkMetrics.mu.Unlock()
	}
}
func (c *tableWorkCollector) Describe(out chan<- *prometheus.Desc) {
	for _, d := range c.descriptions {
		out <- d
	}
}
func (c *tableWorkCollector) Collect(out chan<- prometheus.Metric) {
	c.mu.Lock()
	sources := make(map[string]func() map[wal.Destination]TableWork, len(c.sources))
	for pipeline, read := range c.sources {
		sources[pipeline] = read.read
	}
	c.mu.Unlock()
	for pipeline, read := range sources {
		for d, s := range read() {
			values := []float64{float64(s.Queued), float64(s.Running), max(0, time.Until(s.RetryAt).Seconds())}
			for n, value := range values {
				out <- prometheus.MustNewConstMetric(c.descriptions[n], prometheus.GaugeValue, value, pipeline, d.Database, d.Table)
			}
		}
	}
}
