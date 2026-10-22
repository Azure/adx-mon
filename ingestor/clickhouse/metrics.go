package clickhouse

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	resultSuccess   = "success"
	resultRetryable = "retryable_error"
	resultFatal     = "fatal_error"
)

var (
	uploadAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "adx_mon",
		Subsystem: "ingestor_clickhouse",
		Name:      "uploads_total",
		Help:      "Total number of ClickHouse batch upload attempts grouped by result.",
	}, []string{"database", "result"})

	uploadRows = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "adx_mon",
		Subsystem: "ingestor_clickhouse",
		Name:      "uploaded_rows_total",
		Help:      "Total number of rows successfully uploaded to ClickHouse.",
	}, []string{"database"})

	uploadBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "adx_mon",
		Subsystem: "ingestor_clickhouse",
		Name:      "uploaded_bytes_total",
		Help:      "Total number of WAL bytes successfully uploaded to ClickHouse.",
	}, []string{"database"})

	uploadLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "adx_mon",
		Subsystem: "ingestor_clickhouse",
		Name:      "upload_duration_seconds",
		Help:      "Latency of ClickHouse upload operations.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"database"})
)

type uploadStats struct {
	database string
	rows     int
	bytes    int64
	result   string
	started  time.Time
}

func newUploadStats(database string) *uploadStats {
	return &uploadStats{
		database: database,
		result:   resultSuccess,
		started:  time.Now(),
	}
}

func (s *uploadStats) addBytes(b int64) {
	s.bytes += b
}

func (s *uploadStats) setRows(rows int) {
	s.rows = rows
}

func (s *uploadStats) markFailure(retryable bool) {
	if retryable {
		if s.result == resultSuccess {
			s.result = resultRetryable
		}
	} else {
		s.result = resultFatal
	}
}

func (s *uploadStats) error(err error, retryable bool) error {
	s.markFailure(retryable)
	return err
}

func (s *uploadStats) observe() {
	uploadAttempts.WithLabelValues(s.database, s.result).Inc()
	if s.result == resultSuccess {
		if s.rows > 0 {
			uploadRows.WithLabelValues(s.database).Add(float64(s.rows))
		}
		if s.bytes > 0 {
			uploadBytes.WithLabelValues(s.database).Add(float64(s.bytes))
		}
		uploadLatency.WithLabelValues(s.database).Observe(time.Since(s.started).Seconds())
	}
}
