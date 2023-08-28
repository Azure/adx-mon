package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
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

func MeasureHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := statusRecorder{w, 200, 0}
		path := getPath(r)
		InflightRequests.WithLabelValues(path).Inc()

		defer func() {
			if p := recover(); p != nil {
				logger.Error("recovered from panic in request handler %s: %v", path, p)
				rec.statusCode = http.StatusInternalServerError
				rec.WriteHeader(http.StatusInternalServerError)
			}
			record(r, &rec, start)
		}()

		next.ServeHTTP(&rec, r)
	}
}

func record(r *http.Request, rec *statusRecorder, start time.Time) {
	path := getPath(r)
	status := strconv.Itoa(rec.statusCode)
	RequestsReceived.WithLabelValues(path, status).Inc()
	RequestDurationSeconds.WithLabelValues(path).Observe(time.Since(start).Seconds())
	RequestBytesReceived.WithLabelValues(path).Observe(float64(r.ContentLength))
	InflightRequests.WithLabelValues(path).Dec()
	ResponseBytesSent.WithLabelValues(path).Observe(float64(rec.respSize))
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
	HttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Inc()
	logger.Info("making request to %s", r.URL)
	resp, err := rt.next.RoundTrip(r)
	latency := time.Since(start)
	logger.Info("completed request to %s in %ds with status code: %d", r.URL, latency.Seconds(), resp.StatusCode)

	HttpRequestLatency.WithLabelValues(r.URL.Host, r.URL.Path).Observe(latency.Seconds())
	HttpRequestsBytesSent.WithLabelValues(r.URL.Host, r.URL.Path).Observe(float64(r.ContentLength))
	HttpRequestsInFlight.WithLabelValues(r.URL.Host, r.URL.Path).Dec()
	HttpRequestsTotal.WithLabelValues(r.URL.Host, r.URL.Path, strconv.Itoa(resp.StatusCode)).Inc()
	HttpResponseBytesReceived.WithLabelValues(r.URL.Host, r.URL.Path).Observe(float64(resp.ContentLength))
	return resp, err
}
