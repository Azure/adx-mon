package main

import (
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type snapshot struct {
	durationSeconds, maxDurationSeconds  float64
	attempts                             map[outcome]uint64
	responses                            map[int]uint64
	disconnects, readErrors, writeErrors uint64
	cancellations, clientCancellations   uint64
	receivedBytes, acceptedBytes         uint64
	inflight                             int64
	finished                             uint64
}

type statistics struct {
	mu       sync.Mutex
	data     snapshot
	started  time.Time
	duration prometheus.Histogram
	descs    map[string]*prometheus.Desc
}

func newStatistics() *statistics {
	s := &statistics{
		started: time.Now(),
		data:    snapshot{attempts: map[outcome]uint64{accept: 0, reject: 0, disconnect: 0}, responses: make(map[int]uint64)},
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "mock_ingestor_transfer_duration_seconds", Help: "Transfer handler duration, including body reads and response delay.",
			Buckets: []float64{.001, .01, .1, .5, 1, 5, 10, 20, 30, 60},
		}),
		descs: make(map[string]*prometheus.Desc),
	}
	for name, help := range map[string]string{
		"disconnects_total":              "Actual policy-generated hijacked connection closures.",
		"body_read_errors_total":         "Transfer body read errors, including interrupted uploads.",
		"response_write_errors_total":    "Transfer response flush errors or unsupported hijacking.",
		"cancellations_total":            "Canceled transfer contexts, including shutdown.",
		"client_cancellations_total":     "Transfers whose client request context was canceled.",
		"received_body_bytes_total":      "Encoded body bytes consumed, excluding HTTP framing (including partial uploads).",
		"accepted_body_bytes_total":      "Encoded body bytes with a fully consumed upload and successfully flushed 202 (not proof of client receipt).",
		"inflight":                       "Currently executing transfer handlers.",
		"finished_total":                 "Finished transfer handlers, including malformed attempts.",
		"uptime_seconds":                 "Time since statistics initialization.",
		"transfers_per_second":           "Lifetime average finished transfer handlers per second.",
		"received_body_bytes_per_second": "Lifetime average encoded body bytes consumed per second.",
		"accepted_body_bytes_per_second": "Lifetime average accepted encoded body bytes per second.",
	} {
		s.descs[name] = prometheus.NewDesc("mock_ingestor_"+name, help, nil, nil)
	}
	s.descs["attempts_total"] = prometheus.NewDesc("mock_ingestor_attempts_total", "Valid transfer attempts by selected policy outcome, not completed deliveries.", []string{"outcome"}, nil)
	s.descs["responses_total"] = prometheus.NewDesc("mock_ingestor_responses_total", "Successfully flushed transfer HTTP responses (not proof of client receipt).", []string{"status"}, nil)
	return s
}

func (s *statistics) update(f func(*snapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.data)
}

func (s *statistics) snapshot() snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.data
	x.attempts = make(map[outcome]uint64, len(s.data.attempts))
	for k, v := range s.data.attempts {
		x.attempts[k] = v
	}
	x.responses = make(map[int]uint64, len(s.data.responses))
	for k, v := range s.data.responses {
		x.responses[k] = v
	}
	return x
}

func (s *statistics) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range s.descs {
		ch <- d
	}
	s.duration.Describe(ch)
}

func (s *statistics) Collect(ch chan<- prometheus.Metric) {
	x := s.snapshot()
	for k, v := range x.attempts {
		ch <- prometheus.MustNewConstMetric(s.descs["attempts_total"], prometheus.CounterValue, float64(v), string(k))
	}
	for k, v := range x.responses {
		ch <- prometheus.MustNewConstMetric(s.descs["responses_total"], prometheus.CounterValue, float64(v), strconv.Itoa(k))
	}
	for name, v := range map[string]uint64{
		"disconnects_total": x.disconnects, "body_read_errors_total": x.readErrors, "response_write_errors_total": x.writeErrors,
		"cancellations_total": x.cancellations, "client_cancellations_total": x.clientCancellations,
		"received_body_bytes_total": x.receivedBytes, "accepted_body_bytes_total": x.acceptedBytes, "finished_total": x.finished,
	} {
		ch <- prometheus.MustNewConstMetric(s.descs[name], prometheus.CounterValue, float64(v))
	}
	elapsed := time.Since(s.started).Seconds()
	for name, v := range map[string]float64{
		"inflight": float64(x.inflight), "uptime_seconds": elapsed,
		"transfers_per_second":           float64(x.finished) / elapsed,
		"received_body_bytes_per_second": float64(x.receivedBytes) / elapsed,
		"accepted_body_bytes_per_second": float64(x.acceptedBytes) / elapsed,
	} {
		ch <- prometheus.MustNewConstMetric(s.descs[name], prometheus.GaugeValue, v)
	}
	s.duration.Collect(ch)
}

func (s *statistics) report(logger *log.Logger, kind string, previous snapshot, since time.Time) snapshot {
	x := s.snapshot()
	seconds := time.Since(since).Seconds()
	meanDuration := float64(0)
	if finished := x.finished - previous.finished; finished > 0 {
		meanDuration = (x.durationSeconds - previous.durationSeconds) / float64(finished)
	}
	logger.Printf("stats=%s elapsed=%s accept_selected=%d reject_selected=%d disconnect_selected=%d responses=%v disconnects=%d read_errors=%d write_errors=%d cancellations=%d client_cancellations=%d received_bytes=%d accepted_bytes=%d inflight=%d transfers_per_sec=%.2f received_bytes_per_sec=%.2f accepted_bytes_per_sec=%.2f mean_duration_seconds=%.6f lifetime_max_duration_seconds=%.6f",
		kind, time.Since(s.started).Round(time.Millisecond), x.attempts[accept], x.attempts[reject], x.attempts[disconnect], x.responses,
		x.disconnects, x.readErrors, x.writeErrors, x.cancellations, x.clientCancellations, x.receivedBytes, x.acceptedBytes, x.inflight,
		float64(x.finished-previous.finished)/seconds, float64(x.receivedBytes-previous.receivedBytes)/seconds, float64(x.acceptedBytes-previous.acceptedBytes)/seconds, meanDuration, x.maxDurationSeconds)
	return x
}
