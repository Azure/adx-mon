package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
	respSize   int
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	rec.respSize += len(b)
	return rec.ResponseWriter.Write(b)
}

func (rec *statusRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

type handlerRecorder struct {
	// Generic HTTP  metrics
	InflightRequests       *prometheus.GaugeVec
	RequestsReceived       *prometheus.CounterVec
	RequestDurationSeconds *prometheus.HistogramVec
	RequestBytesReceived   *prometheus.HistogramVec
	ResponseBytesSent      *prometheus.HistogramVec
}

func (h *handlerRecorder) registerMetrics(subsystem string) {
	switch subsystem {
	case IngestorSubsystem:
		h.InflightRequests = IngestorInflightRequests
		h.RequestsReceived = IngestorRequestsReceived
		h.RequestDurationSeconds = IngestorRequestDurationSeconds
		h.RequestBytesReceived = IngestorRequestBytesReceived
		h.ResponseBytesSent = IngestorResponseBytesSent
	case CollectorSubsystem:
		h.InflightRequests = CollectorInflightRequests
		h.RequestsReceived = CollectorRequestsReceived
		h.RequestDurationSeconds = CollectorRequestDurationSeconds
		h.RequestBytesReceived = CollectorRequestBytesReceived
		h.ResponseBytesSent = CollectorResponseBytesSent
	default:
		h.InflightRequests = InflightRequests
		h.RequestsReceived = RequestsReceived
		h.RequestDurationSeconds = RequestDurationSeconds
		h.RequestBytesReceived = RequestBytesReceived
		h.ResponseBytesSent = ResponseBytesSent
	}
}

func HandlerFuncRecorder(subsystem string, next http.HandlerFunc) http.HandlerFunc {
	h := &handlerRecorder{}
	h.registerMetrics(subsystem)
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := statusRecorder{w, 200, 0}
		path := getPath(r)
		logger.Debugf("recieved request to %s", path)
		h.InflightRequests.WithLabelValues(path).Inc()

		next.ServeHTTP(&rec, r)
		logger.Debugf("completed request to %s with status code %d", path, rec.statusCode)
		status := strconv.Itoa(rec.statusCode)
		h.RequestsReceived.WithLabelValues(path, status).Inc()
		h.RequestDurationSeconds.WithLabelValues(path).Observe(time.Since(start).Seconds())
		h.RequestBytesReceived.WithLabelValues(path).Observe(float64(r.ContentLength))
		h.ResponseBytesSent.WithLabelValues(path).Observe(float64(rec.respSize))
		h.InflightRequests.WithLabelValues(path).Dec()
	}
}

func getPath(r *http.Request) string {
	return r.URL.Path
}

type roundTripper struct {
	next http.RoundTripper
}

// TODO: consider using this for scraping...
func NewRoundTripper(next http.RoundTripper) http.RoundTripper {
	return &roundTripper{next: next}
}

func (rt *roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	ClientHttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Inc()
	logger.Debugf("making request to %s", r.URL)
	resp, err := rt.next.RoundTrip(r)
	latency := time.Since(start)

	var statusCode string
	var contentLength float64
	if resp != nil {
		statusCode = strconv.Itoa(resp.StatusCode)
		contentLength = float64(resp.ContentLength)
	} else if err != nil {
		statusCode = "500"
	}

	logger.Debugf("completed request to %s in %s with status code: %s", r.URL, latency, statusCode)

	ClientHttpRequestLatency.WithLabelValues(r.URL.Host, r.URL.Path).Observe(latency.Seconds())
	ClientHttpRequestsBytesSent.WithLabelValues(r.URL.Host, r.URL.Path).Observe(float64(r.ContentLength))
	ClientHttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Dec()
	ClientHttpRequestsTotal.WithLabelValues(r.URL.Host, r.URL.Path, statusCode).Inc()
	ClientHttpResponseBytesReceived.WithLabelValues(r.URL.Host, r.URL.Path).Observe(contentLength)
	return resp, err
}
