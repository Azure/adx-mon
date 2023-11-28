package metrics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	bytesBufPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})

	bytesPool = pool.NewBytes(1000)

	writeReqPool = pool.NewGeneric(1000, func(sz int) interface{} {
		return prompb.WriteRequest{
			Timeseries: make([]prompb.TimeSeries, 0, sz),
		}
	})
)

type SeriesCounter interface {
	AddSeries(key string, id uint64)
}

type RequestWriter interface {
	// Write writes the time series to the correct peer.
	Write(ctx context.Context, wr prompb.WriteRequest) error
}

type HealthChecker interface {
	IsHealthy() bool
}

type HandlerOpts struct {
	// Path is the path where the handler will be registered.
	Path string

	RequestTransformer interface {
		TransformWriteRequest(req prompb.WriteRequest) prompb.WriteRequest
	}

	// RequestWriter is the interface that writes the time series to a destination.
	RequestWriter RequestWriter

	// Health is the interface that determines if the service is healthy.
	HealthChecker HealthChecker

	// Database is the name of the Kusto database where time series will be written.
	Database string
}

type Handler struct {
	Path string

	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	requestTransformer interface {
		TransformWriteRequest(req prompb.WriteRequest) prompb.WriteRequest
	}

	requestWriter RequestWriter
	database      string
	health        HealthChecker
}

func (s *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.HandleReceive(writer, request)
}

func NewHandler(opts HandlerOpts) *Handler {
	return &Handler{
		Path:               opts.Path,
		health:             opts.HealthChecker,
		requestTransformer: opts.RequestTransformer,
		requestWriter:      opts.RequestWriter,
		database:           opts.Database,
	}
}

// HandleReceive handles the prometheus remote write requests and writes them to the store.
func (s *Handler) HandleReceive(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/receive"})
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Errorf("close http body: %s", err.Error())
		}
	}()

	if !s.health.IsHealthy() {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	}

	bodyBuf := bytesBufPool.Get(64 * 1024).(*bytes.Buffer)
	defer bytesBufPool.Put(bodyBuf)
	bodyBuf.Reset()

	_, err := io.Copy(bodyBuf, r.Body)
	if err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	compressed := bodyBuf.Bytes()
	buf := bytesPool.Get(64 * 1024)
	defer bytesPool.Put(buf)
	buf = buf[:0]

	reqBuf, err := snappy.Decode(buf, compressed)
	if err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req := writeReqPool.Get(2500).(prompb.WriteRequest)
	defer writeReqPool.Put(req)
	req.Reset()

	if err := req.Unmarshal(reqBuf); err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusBadRequest)).Inc()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Apply any label or metrics drops or additions
	req = s.requestTransformer.TransformWriteRequest(req)

	if len(req.Timeseries) == 0 {
		m.WithLabelValues(strconv.Itoa(http.StatusNoContent)).Inc()
		w.WriteHeader(http.StatusAccepted)
		return
	}

	err = s.requestWriter.Write(r.Context(), req)
	if errors.Is(err, wal.ErrMaxSegmentsExceeded) || errors.Is(err, wal.ErrMaxDiskUsageExceeded) {
		m.WithLabelValues(strconv.Itoa(http.StatusTooManyRequests)).Inc()
		http.Error(w, "Overloaded. Retry later", http.StatusTooManyRequests)
		return
	} else if err != nil {
		logger.Errorf("Failed to write ts: %s", err.Error())
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
	w.WriteHeader(http.StatusAccepted)
}
