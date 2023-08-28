package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Namespace = "adxmon"

	// Generic HTTP  metrics
	InflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "http_server",
		Name:      "in_flight_requests",
	}, []string{"path"})

	RequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "http_server",
		Name:      "requests_total",
		Help:      "Counter of requests received for this http server",
	}, []string{"path", "code"})

	RequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_server",
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"path"})

	RequestBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_server",
		Name:      "request_bytes",
		Help:      "A histogram of request sizes from the wrapped server.",
	}, []string{"path"})

	ResponseBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_server",
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped server.",
	}, []string{"path"})

	HttpRequestsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "http_client",
		Name:      "in_flight_requests",
		Help:      "A gauge of in-flight requests for the wrapped client.",
	}, []string{"host", "path"})

	HttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "http_client",
		Name:      "requests_total",
		Help:      "A counter for requests from the wrapped client.",
	}, []string{"host", "path", "code"})

	HttpRequestsBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_client",
		Name:      "request_bytes",
		Help:      "A histogram of request sizes for requests from the wrapped client.",
	}, []string{"host", "path"})

	HttpResponseBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_client",
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped client.",
	}, []string{"host", "path"})

	HttpRequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http_client",
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"host", "path"})

	// Ingestor metrics
	IngestorUploadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "upload_errors_total",
		Help:      "Counter of upload errors for an ingestor instance",
	})
	IngestorWalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "wal_errors_total",
		Help:      "Counter of errors related to WAL IO for an ingestor instance",
	}, []string{"error"})
	IngestorInternalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "internal_errors_total",
		Help:      "Counter of internal errors for an ingestor instance",
	}, []string{"error"})

	FileUploadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_uploads_total",
		Help:      "Counter of file uploads for an ingestor instance",
	}, []string{"metric", "reason", "owned"})

	FileUploadBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_size_upload_bytes",
		Help:      "Histogram of the size of files uploaded",
	}, []string{"metric"})

	FileUploadAge = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_upload_age_seconds",
		Help:      "Histogram of the age of files uploaded",
	}, []string{"metric"})

	FileTransferTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_transfers_total",
		Help:      "Counter of file transfers for an ingestor instance",
	}, []string{"metric", "reason", "owned"})

	FileTransferBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_size_transfer_bytes",
		Help:      "Histogram of the size of files transfered",
	}, []string{"metric"})

	FileTransferAge = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "file_transfer_age_seconds",
		Help:      "Histogram of the age of files transfered",
	}, []string{"metric"})

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

	IngestorSegmentsCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
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

	LogsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "ingestor",
		Name:      "logs_received",
		Help:      "Counter of the number of logs received",
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
	AlerterErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "alerter",
		Name:      "errors_total",
		Help:      "Counter of errors for an alerter instance, broken down by rule and error type",
	}, []string{"rule", "error"})

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

	// Collector metrics
	LogsProxyReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "logs_received",
		Help:      "Counter of the number of logs received by the proxy",
	})

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

	CollectorErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "collector",
		Name:      "errors_total",
		Help:      "Counter of errors for a collector instance, broken down by error type",
	}, []string{"error"})
)
