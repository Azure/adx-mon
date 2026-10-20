package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// IntakeMemory reports the bytes buffered by in-flight transfer requests and
// the configured ceiling, where zero means the ceiling is disabled.
type IntakeMemory interface {
	InFlight() int64
	Limit() int64
}

// intakeCollector exports intake memory so operators can size the ceiling from
// real traffic. Usage is tracked even when the ceiling is disabled.
type intakeCollector struct {
	mu      sync.Mutex
	sources map[string]IntakeMemory
	inUse   *prometheus.Desc
	limit   *prometheus.Desc
}

func newIntakeCollector() *intakeCollector {
	return &intakeCollector{
		sources: make(map[string]IntakeMemory),
		inUse:   prometheus.NewDesc(Namespace+"_intake_memory_bytes", "Bytes buffered by in-flight transfer requests.", []string{"component"}, nil),
		limit:   prometheus.NewDesc(Namespace+"_intake_memory_limit_bytes", "Ceiling on bytes buffered by in-flight transfer requests; zero when disabled.", []string{"component"}, nil),
	}
}

var intakeMemory = newIntakeCollector()

func init() { prometheus.MustRegister(intakeMemory) }

func RegisterIntakeMemory(component string, m IntakeMemory) func() {
	intakeMemory.mu.Lock()
	intakeMemory.sources[component] = m
	intakeMemory.mu.Unlock()
	return func() {
		intakeMemory.mu.Lock()
		defer intakeMemory.mu.Unlock()
		if intakeMemory.sources[component] == m {
			delete(intakeMemory.sources, component)
		}
	}
}

func (c *intakeCollector) Describe(out chan<- *prometheus.Desc) {
	out <- c.inUse
	out <- c.limit
}

func (c *intakeCollector) Collect(out chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for component, m := range c.sources {
		out <- prometheus.MustNewConstMetric(c.inUse, prometheus.GaugeValue, float64(m.InFlight()), component)
		out <- prometheus.MustNewConstMetric(c.limit, prometheus.GaugeValue, float64(m.Limit()), component)
	}
}
