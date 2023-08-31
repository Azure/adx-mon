package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

func (rec *handlerRecorder) registerMetrics(subsystem string) {
	// Generic HTTP  metrics
	rec.InflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "in_flight_requests",
	}, []string{"path"})

	rec.RequestsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "requests_total",
		Help:      "Counter of requests received for this http server",
	}, []string{"path", "code"})

	rec.RequestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"path"})

	rec.RequestBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "request_bytes",
		Help:      "A histogram of request sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})

	rec.ResponseBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped server.",
		Buckets:   bytesBucket,
	}, []string{"path"})
}

func HandlerFuncRecorder(subsystem string, next http.HandlerFunc) http.HandlerFunc {
	h := &handlerRecorder{}
	h.registerMetrics(subsystem)
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := statusRecorder{w, 200, 0}
		path := getPath(r)
		logger.Debug("recieved request to %s", path)
		h.InflightRequests.WithLabelValues(path).Inc()

		next.ServeHTTP(&rec, r)
		logger.Debug("completed request to %s with status code %d", path, rec.statusCode)
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

	HttpRequestsInFlight      *prometheus.GaugeVec
	HttpRequestsTotal         *prometheus.CounterVec
	HttpRequestsBytesSent     *prometheus.HistogramVec
	HttpResponseBytesReceived *prometheus.HistogramVec
	HttpRequestLatency        *prometheus.HistogramVec
}

// TODO: consider using this for scraping...
func NewRoundTripper(subsystem string, next http.RoundTripper) http.RoundTripper {
	rt := &roundTripper{next: next}
	rt.registerMetrics(subsystem)
	return rt
}

func (rt *roundTripper) registerMetrics(subsystem string) {
	rt.HttpRequestsInFlight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "in_flight_requests",
		Help:      "A gauge of in-flight requests for the wrapped client.",
	}, []string{"host", "path"})

	rt.HttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "requests_total",
		Help:      "A counter for requests from the wrapped client.",
	}, []string{"host", "path", "code"})

	rt.HttpRequestsBytesSent = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "request_bytes",
		Help:      "A histogram of request sizes for requests from the wrapped client.",
		Buckets:   bytesBucket,
	}, []string{"host", "path"})

	rt.HttpResponseBytesReceived = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "response_bytes",
		Help:      "A histogram of response sizes from the wrapped client.",
		Buckets:   bytesBucket,
	}, []string{"host", "path"})

	rt.HttpRequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Subsystem: subsystem,
		Name:      "request_duration_seconds",
		Help:      "A histogram of request latencies.",
	}, []string{"host", "path"})
}

func (rt *roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	rt.HttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Inc()
	logger.Debug("making request to %s", r.URL)
	resp, err := rt.next.RoundTrip(r)
	latency := time.Since(start)
	logger.Debug("completed request to %s in %ds with status code: %d", r.URL, latency.Seconds(), resp.StatusCode)

	rt.HttpRequestLatency.WithLabelValues(r.URL.Host, r.URL.Path).Observe(latency.Seconds())
	rt.HttpRequestsBytesSent.WithLabelValues(r.URL.Host, r.URL.Path).Observe(float64(r.ContentLength))
	rt.HttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Dec()
	rt.HttpRequestsTotal.WithLabelValues(r.URL.Host, r.URL.Path, strconv.Itoa(resp.StatusCode)).Inc()
	rt.HttpResponseBytesReceived.WithLabelValues(r.URL.Host, r.URL.Path).Observe(float64(resp.ContentLength))
	return resp, err
}
