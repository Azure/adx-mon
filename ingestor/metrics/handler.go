package metrics

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/cespare/xxhash"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	bytesBufPool = pool.NewGeneric(50, func(sz int) interface{} {
		return bytes.NewBuffer(make([]byte, 0, sz))
	})

	bytesPool = pool.NewBytes(50)

	writeReqPool = pool.NewGeneric(50, func(sz int) interface{} {
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

type HandlerOpts struct {
	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	// SeriesCounter is an optional interface that can be used to count the number of series ingested.
	SeriesCounter SeriesCounter

	// RequestWriter is the interface that writes the time series to a destination.
	RequestWriter RequestWriter
}

type Handler struct {
	// DropLabels is a map of metric names regexes to label name regexes.  When both match, the label will be dropped.
	DropLabels map[*regexp.Regexp]*regexp.Regexp

	// DropMetrics is a slice of regexes that drops metrics when the metric name matches.  The metric name format
	// should match the Prometheus naming style before the metric is translated to a Kusto table name.
	DropMetrics []*regexp.Regexp

	requestFilter interface {
		Filter(req prompb.WriteRequest) prompb.WriteRequest
	}

	seriesCounter SeriesCounter

	requestWriter RequestWriter
}

func (s *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.HandleReceive(writer, request)
}

func NewHandler(opts HandlerOpts) *Handler {
	return &Handler{
		requestFilter: &transform.RequestFilter{
			DropMetrics: opts.DropMetrics,
			DropLabels:  opts.DropLabels,
		},
		seriesCounter: opts.SeriesCounter,
		requestWriter: opts.RequestWriter,
	}
}

// HandleReceive handles the prometheus remote write requests and writes them to the store.
func (s *Handler) HandleReceive(w http.ResponseWriter, r *http.Request) {
	m := metrics.RequestsReceived.MustCurryWith(prometheus.Labels{"path": "/receive"})
	defer func() {
		if err := r.Body.Close(); err != nil {
			logger.Error("close http body: %s", err.Error())
		}
	}()

	bodyBuf := bytesBufPool.Get(1024 * 1024).(*bytes.Buffer)
	defer bytesBufPool.Put(bodyBuf)
	bodyBuf.Reset()

	_, err := io.Copy(bodyBuf, r.Body)
	if err != nil {
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	compressed := bodyBuf.Bytes()
	buf := bytesPool.Get(1024 * 1024)
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

	if len(s.DropMetrics) > 0 || len(s.DropLabels) > 0 {
		req = s.filterDropMetrics(req)
	}

	if s.seriesCounter != nil {
		seriesId := xxhash.New()
		for _, v := range req.Timeseries {
			seriesId.Reset()
			var metric string

			// Labels should already be sorted by name, but just in case they aren't, sort them.
			if !prompb.IsSorted(v.Labels) {
				prompb.Sort(v.Labels)
			}

			for _, vv := range v.Labels {
				if bytes.Equal(vv.Name, []byte("__name__")) {
					metric = string(vv.Value)
				}
				seriesId.Write(vv.Name)
				seriesId.Write(vv.Value)
			}
			s.seriesCounter.AddSeries(metric, seriesId.Sum64())
		}
	}

	if err := s.requestWriter.Write(r.Context(), req); err != nil {
		logger.Error("Failed to write ts: %s", err.Error())
		m.WithLabelValues(strconv.Itoa(http.StatusInternalServerError)).Inc()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.WithLabelValues(strconv.Itoa(http.StatusAccepted)).Inc()
	w.WriteHeader(http.StatusAccepted)
}

// filterDropMetrics remove metrics and labels configured to be dropped by slicing them out
// of the passed prombpWriteRequest.  The modified request is returned to caller.
func (s *Handler) filterDropMetrics(req prompb.WriteRequest) prompb.WriteRequest {
	return s.requestFilter.Filter(req)
}
