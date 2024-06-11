package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Namespace = "adxmon"

	// Ingestor metrics
	IngestorHealthCheck = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "health_check",
		Help:      "Gauge indicating if Ingestor is healthy or not",
	}, []string{"region"})

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

	IngestorQueueSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "queue_size",
		Help:      "Gauge indicating the size of the queue for an ingestor instance",
	}, []string{"queue"})

	IngestorSegmentsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "wal_segments_count",
		Help:      "Gauge indicating the number of WAL segments for an ingestor instance",
	}, []string{"metric"})

	IngestorSegmentsSizeBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "wal_segments_size_bytes",
		Help:      "Gauge indicating the size of WAL segments for an ingestor instance",
	}, []string{"metric"})

	IngestorSegmentsMaxAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "wal_segments_max_age_seconds",
		Help:      "Gauge indicating the max age of WAL segments for an ingestor instance",
	}, []string{"metric"})

	MetricsDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "metrics_dropped_total",
		Help:      "Counter of metrics droopped for an ingestor instance",
	}, []string{"metric"})

	MetricsUploaded = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "metrics_uploaded_total",
		Help:      "Counter of the number of metrics uploaded to Kusto",
	}, []string{"database", "table"})

	LogsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "logs_received_total",
		Help:      "Counter of the number of logs received",
	}, []string{"database", "table"})

	LogsUploaded = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "logs_uploaded_total",
		Help:      "Counter of the number of logs uploaded to Kusto",
	}, []string{"database", "table"})

	InvalidLogsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "invalid_logs_dropped",
		Help:      "Counter of the number of invalid logs dropped",
	}, []string{})

	ValidLogsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "valid_logs_dropped",
		Help:      "Counter of the number of logs dropped due to ingestor errors",
	}, []string{})

	// Alerting metrics
	AlerterHealthCheck = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "health_check",
		Help:      "Gauge indicating if Alerter is healthy or not",
	}, []string{"location"})

	QueryHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "query_health",
		Help:      "Gauge indicating if a query is healthy or not",
	}, []string{"namespace", "name"})

	QueriesRunTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "queries_run_total",
		Help:      "Counter of the number of queries run",
	}, []string{})

	NotificationUnhealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "notification_unhealthy",
		Help:      "Gauge indicating if a notification is healthy or not",
	}, []string{"namespace", "name"})

	// Collector metrics
	CollectorHealthCheck = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "health_check",
		Help:      "Gauge indicating if Collector is healthy or not",
	}, []string{"region"})

	LogsProxyReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_received_total",
		Help:      "Counter of the number of logs received by the proxy",
	}, []string{"database", "table"})

	LogsProxyUploaded = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_uploaded_total",
		Help:      "Counter of the number of logs uploaded",
	}, []string{"database", "table"})

	LogsProxyFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_failures",
		Help:      "Counter of the number of failures when proxying logs to the OTLP endpoints",
	}, []string{"endpoint"})

	LogsProxyPartialFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_partial_failures",
		Help:      "Counter of the number of partial failures when proxying logs to the OTLP endpoints",
	}, []string{"endpoint"})

	LogKeys = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_keys",
		Help:      "Number of keys found in logs",
	}, []string{"database", "table"})

	LogSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_size",
		Help:      "Size of logs in bytes",
	}, []string{"database", "table"})

	LogsCollectorLogsCollected = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_collected",
		Help:      "Counter of the number of logs collected by the collector",
	}, []string{"source"})

	LogsCollectorLogsSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_sent",
		Help:      "Counter of the number of logs successfully sent by the collector",
	}, []string{"source", "sink"})

	LogsCollectorLogsDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_dropped",
		Help:      "Counter of the number of logs dropped due to errors",
	}, []string{"source", "stage"})

	MetricsRequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "metrics_requests_received_total",
		Help:      "Counter of the number of metrics requests received",
	}, []string{"protocol", "code", "endpoint"})
)
