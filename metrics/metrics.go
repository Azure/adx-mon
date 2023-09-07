package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	bytesBucket        = prometheus.ExponentialBuckets(100, 10, 8)
	Namespace          = "adxmon"
	IngestorSubsystem  = "ingestor"
	AlerterSubsystem   = "alerter"
	CollectorSubsystem = "collector"

	// Generic HTTP Metrics
	InflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "in_flight_requests",
	}, []string{"path"})

	RequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Counter of requests received for this http server",
	}, []string{"path", "code"})

	RequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"path"})

	RequestBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "request_bytes",
		Help:      "A histogram of request sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	ResponseBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	ClientHttpRequestsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "outbound_in_flight_requests",
		Help:      "A gauge of in-flight requests for the wrapped client.",
	}, []string{"host", "path"})

	ClientHttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "outbound_requests_total",
		Help:      "A counter for requests from the wrapped client.",
	}, []string{"host", "path", "code"})

	ClientHttpRequestsBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "outbound_request_bytes",
		Help:      "A histogram of request sizes for requests from the wrapped client.",
		Buckets:   bytesBucket,
	}, []string{"host", "path"})

	ClientHttpResponseBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "outbound_response_bytes",
		Help:      "A histogram of response sizes from the wrapped client.",
		Buckets:   bytesBucket,
	}, []string{"host", "path"})

	ClientHttpRequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: "http",
		Name:      "outbound_request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"host", "path"})

	// TODO: Consider collapsing http metrics into a single subsystem, and dileniate usage by scrape job/pod name.
	// Ingestor HTTP Metrics
	IngestorInflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "in_flight_requests",
	}, []string{"path"})

	IngestorRequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "requests_total",
		Help:      "Counter of requests received for this http server",
	}, []string{"path", "code"})

	IngestorRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"path"})

	IngestorRequestBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "request_bytes",
		Help:      "A histogram of request sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	IngestorResponseBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	// Collector HTTP Metrics
	CollectorInflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "in_flight_requests",
	}, []string{"path"})

	CollectorRequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "requests_total",
		Help:      "Counter of requests received for this http server",
	}, []string{"path", "code"})

	CollectorRequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"path"})

	CollectorRequestBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "request_bytes",
		Help:      "A histogram of request sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	CollectorResponseBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	// Ingestor metrics
	IngestorUploadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "upload_errors_total",
		Help:      "Counter of upload errors for an ingestor instance",
	})
	IngestorWalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "wal_errors_total",
		Help:      "Counter of errors related to WAL IO for an ingestor instance",
	}, []string{"error"})
	IngestorInternalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "internal_errors_total",
		Help:      "Counter of internal errors for an ingestor instance",
	}, []string{"error"})

	SegmentUploadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_uploads_total",
		Help:      "Counter of segment uploads for an ingestor instance",
	}, []string{"metric", "reason", "owned"})

	SegmentUploadBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_size_upload_bytes",
		Help:      "Histogram of the size of segments uploaded",
		Buckets:   bytesBucket,
	}, []string{"metric"})

	SegmentUploadAge = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_upload_age_seconds",
		Help:      "Histogram of the age of segments uploaded",
	}, []string{"metric"})

	SegmentTransferTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_transfers_total",
		Help:      "Counter of segment transfers for an ingestor instance",
	}, []string{"metric", "reason", "owned"})

	SegmentTransferBytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_size_transfer_bytes",
		Help:      "Histogram of the size of segments transfered",
		Buckets:   bytesBucket,
	}, []string{"metric"})

	SegmentTransferAge = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "segment_transfer_age_seconds",
		Help:      "Histogram of the age of segments transfered",
		Buckets:   bytesBucket,
	}, []string{"metric"})

	SamplesStored = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
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
		Subsystem: IngestorSubsystem,
		Name:      "wal_segments_count",
		Help:      "Gauge indicating the number of WAL segments for an ingestor instance",
	}, []string{"metric"})

	IngestorSegmentsSizeBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "wal_segments_size_bytes",
		Help:      "Gauge indicating the size of WAL segments for an ingestor instance",
	}, []string{"metric"})

	IngestorSegmentsMaxAge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
		Name:      "wal_segments_max_age_seconds",
		Help:      "Gauge indicating the max age of WAL segments for an ingestor instance",
	}, []string{"metric"})

	MetricsDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: IngestorSubsystem,
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
		Subsystem: AlerterSubsystem,
		Name:      "errors_total",
		Help:      "Counter of errors for an alerter instance, broken down by rule and error type",
	}, []string{"rule", "error"})

	QueryHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: AlerterSubsystem,
		Name:      "query_health",
		Help:      "Gauge indicating if a query is healthy or not",
	}, []string{"namespace", "name"})

	NotificationUnhealthy = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: AlerterSubsystem,
		Name:      "notification_unhealthy",
		Help:      "Gauge indicating if a notification is healthy or not",
	}, []string{"namespace", "name"})

	// Collector metrics
	LogsProxyReceived = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "logs_received",
		Help:      "Counter of the number of logs received by the proxy",
	})

	LogsProxyFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "logs_failures",
		Help:      "Counter of the number of failures when proxying logs to the OTLP endpoints",
	}, []string{"endpoint"})

	LogsProxyPartialFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "logs_partial_failures",
		Help:      "Counter of the number of partial failures when proxying logs to the OTLP endpoints",
	}, []string{"endpoint"})

	CollectorErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: CollectorSubsystem,
		Name:      "errors_total",
		Help:      "Counter of errors for a collector instance, broken down by error type",
	}, []string{"error"})
)
