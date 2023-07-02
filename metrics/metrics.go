package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Namespace = "adxmon"

	// Ingestor metrics
	RequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "requests_received_total",
		Help:      "Counter of requests received from an ingestor instance",
	}, []string{"path", "code"})

	SamplesStored = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "samples_stored_total",
		Help:      "Counter of samples stored for an ingestor instance",
	}, []string{"metric"})

	IngestorSegmentsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "wal_segments_count",
		Help:      "Gauge indicating the number of WAL segments for an ingestor instance",
	}, []string{"metric"})

	MetricsDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "metrics_dropped_total",
		Help:      "Counter of metrics droopped for an ingestor instance",
	}, []string{"metric"})

	// Alerting metrics
	QueryHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "query_health",
		Help:      "Gauge indicating if a query is healthy or not",
	}, []string{"namespace", "name"})

	NotificationUnhealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "notification_unhealthy",
		Help:      "Gauge indicating if a notification is healthy or not",
	}, []string{"namespace", "name"})
)
