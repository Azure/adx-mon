package otlp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
	"golang.org/x/sync/errgroup"
)

var (
	// ErrUnknownMetricType is returned when a metric type is not known by this implementation.
	ErrUnknownMetricType = errors.New("received unknown metric type")
)

// ErrRejectedMetric is returned when a write is successful, but some metrics were rejected because they were not able to be handled.
// Example situation is sending unsupported metric types.
type ErrRejectedMetric struct {
	Msg   string
	Count int64
}

func (e *ErrRejectedMetric) Error() string {
	return e.Msg
}

// ErrWriteError is returned when a write fails to the remote. Retriable type of error.
type ErrWriteError struct {
	Err error
}

func (e *ErrWriteError) Error() string {
	return fmt.Sprintf("write error: %s", e.Err.Error())
}

func (e *ErrWriteError) Unwrap() error {
	return e.Err
}

func writeErr(err error) error {
	return &ErrWriteError{
		Err: err,
	}
}

type MetricWriter interface {
	Write(ctx context.Context, msg *v1.ExportMetricsServiceRequest) error
}

/*
  ExportMetricsServiceRequest
   -> ResourceMetrics
     -> Scope Metrics
	   -> Metrics
	     -> Metric (name, type, data)
	       -> Data (metric of type Gauge, Sum, Histogram, ExponentialHistogram, Summary)
		     -> DataPoints (set of data points for a given metric name. Each point contains the attributes (Labels in our parlance) for the point)

   Each data point independently contains the attributes for the point, and there is no guarantee in the protocol
   that all the attributes for the list of data points in a metric are all unique. This means we may get multiple
   data points for the same metric and set of labels, but a different timestamp. In theory, this means that we need
   to keep track of these attribute combinations and only create a promb series for each unique combination.

   In practice, it appears this case only happens when otel messages are being batched by an upper layer. It also
   does not matter for correctness if we have "too many" series. Instead of paying the cost of consistently tracking
   the unique attribute combinations, we can just create a series for each data point.

   The metrics data model is documented here: https://opentelemetry.io/docs/specs/otel/metrics/data-model/
*/

type OltpMetricWriterOpts struct {
	RequestTransformer *transform.RequestTransformer

	DisableMetricsForwarding bool

	// MaxBatchSize is the maximum number of samples to send in a single batch.
	MaxBatchSize int

	Endpoints []string

	Client *promremote.Client
}

type OltpMetricWriter struct {
	requestTransformer       *transform.RequestTransformer
	endpoints                []string
	remoteClient             *promremote.Client
	maxBatchSize             int
	disableMetricsForwarding bool
}

func NewOltpMetricWriter(opts OltpMetricWriterOpts) *OltpMetricWriter {
	return &OltpMetricWriter{
		requestTransformer:       opts.RequestTransformer,
		endpoints:                opts.Endpoints,
		remoteClient:             opts.Client,
		maxBatchSize:             opts.MaxBatchSize,
		disableMetricsForwarding: opts.DisableMetricsForwarding,
	}
}

// Write takes an OTLP ExportMetricsServiceRequest and writes it to the configured endpoints.
func (t *OltpMetricWriter) Write(ctx context.Context, msg *v1.ExportMetricsServiceRequest) error {
	// Causes allocation. Like in prom remote receiver, create rather than using a bunch of space in pool.
	wr := &prompb.WriteRequest{}

	var rejectedRecordsExpHist int64 = 0
	for _, resourceMetrics := range msg.ResourceMetrics {
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			for _, metric := range scopeMetrics.Metrics {
				var err error

				switch data := metric.Data.(type) {
				// Each data point within a metric contains attributes for unique label combinations.
				// Therefore, each metric maps to n prom series.
				case *metricsv1.Metric_Gauge:
					err = t.addOltpNumberPoints(ctx, metric.Name, data.Gauge.DataPoints, wr)
				case *metricsv1.Metric_Sum:
					err = t.addOltpNumberPoints(ctx, metric.Name, data.Sum.DataPoints, wr)
				case *metricsv1.Metric_Histogram:
					err = t.addOltpHistogramPoints(ctx, metric.Name, data.Histogram.DataPoints, wr)
				case *metricsv1.Metric_Summary:
					err = t.addOltpSummaryPoints(ctx, metric.Name, data.Summary.DataPoints, wr)
				case *metricsv1.Metric_ExponentialHistogram:
					// No widespread support for this complicated protocol. Reject for now.
					rejectedRecordsExpHist += int64(len(data.ExponentialHistogram.DataPoints))
					// TODO metric
				default:
					// Return this as a non-retryable error. Since we don't know about this type,
					// we can't count up the number of rejected records.
					err = ErrUnknownMetricType
				}

				// If we get errors adding or flushing points, bail out immediately.
				if err != nil {
					return fmt.Errorf("OTLP Write failure: %w", err)
				}
			}
		}
	}

	// Flush any remaining points.
	if err := t.sendBatch(ctx, wr); err != nil {
		return fmt.Errorf("OLTP Write flush failure: %w", err)
	}

	if rejectedRecordsExpHist > 0 {
		return &ErrRejectedMetric{
			Msg:   fmt.Sprintf("rejected %d records of unsupported type ExponentialHistogram", rejectedRecordsExpHist),
			Count: rejectedRecordsExpHist,
		}
	}

	return nil
}

func (t *OltpMetricWriter) addSeriesAndFlushIfNecessary(ctx context.Context, wr *prompb.WriteRequest, series prompb.TimeSeries) error {
	series = t.requestTransformer.TransformTimeSeries(series)
	wr.Timeseries = append(wr.Timeseries, series)
	if len(wr.Timeseries) >= t.maxBatchSize {
		if err := t.sendBatch(ctx, wr); err != nil {
			return fmt.Errorf("addSeriesAndFlushIfNecessary flush failure: %w", err)
		}
		wr.Timeseries = wr.Timeseries[:0]
	}

	return nil
}

func (t *OltpMetricWriter) sendBatch(ctx context.Context, wr *prompb.WriteRequest) error {
	if len(wr.Timeseries) == 0 {
		return nil
	}

	if len(t.endpoints) == 0 || logger.IsDebug() {
		var sb strings.Builder
		for _, ts := range wr.Timeseries {
			sb.Reset()
			for i, l := range ts.Labels {
				sb.Write(l.Name)
				sb.WriteString("=")
				sb.Write(l.Value)
				if i < len(ts.Labels)-1 {
					sb.Write([]byte(","))
				}
			}
			sb.Write([]byte(" "))
			for _, s := range ts.Samples {
				logger.Debugf("%s %d %f", sb.String(), s.Timestamp, s.Value)
			}
		}
	}

	if t.disableMetricsForwarding {
		return nil
	}

	start := time.Now()
	defer func() {
		logger.Infof("OLTP Sending %d timeseries to %d endpoints duration=%s", len(wr.Timeseries), len(t.endpoints), time.Since(start))
	}()

	g, gCtx := errgroup.WithContext(ctx)
	for _, endpoint := range t.endpoints {
		endpoint := endpoint
		g.Go(func() error {
			return t.remoteClient.Write(gCtx, endpoint, wr)
		})
	}
	if err := g.Wait(); err != nil {
		return writeErr(err)
	}
	return nil
}

func (t *OltpMetricWriter) addOltpNumberPoints(ctx context.Context, name string, points []*metricsv1.NumberDataPoint, wr *prompb.WriteRequest) error {
	for _, point := range points {
		series := newSeries(name, point.Attributes)
		series.Samples = []prompb.Sample{
			{
				Timestamp: unixNanoToUnixMillis(point.TimeUnixNano),
				Value:     asFloat64(point),
			},
		}
		if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
			return err
		}
	}
	return nil
}

func (t *OltpMetricWriter) addOltpHistogramPoints(ctx context.Context, name string, points []*metricsv1.HistogramDataPoint, wr *prompb.WriteRequest) error {
	for _, point := range points {
		timestamp := unixNanoToUnixMillis(point.TimeUnixNano)

		// Add count series
		series := newSeries(fmt.Sprintf("%s_count", name), point.Attributes)
		series.Samples = []prompb.Sample{
			{
				Timestamp: timestamp,
				Value:     float64(point.Count),
			},
		}
		if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
			return err
		}

		// Add sum series, if present
		if point.Sum != nil {
			series := newSeries(fmt.Sprintf("%s_sum", name), point.Attributes)
			series.Samples = []prompb.Sample{
				{
					Timestamp: timestamp,
					Value:     float64(*point.Sum),
				},
			}
			if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
				return err
			}
		}

		// Add bucket series
		numBuckets := len(point.BucketCounts)
		var upperBound string
		for i := 0; i < numBuckets; i++ {
			// if len(BucketCounts) > 0, then len(ExplicitBounds) + 1 == len(BucketCounts)
			// the last bucket has an upper bound of +Inf
			if i == numBuckets-1 {
				upperBound = "+Inf"
			} else {
				upperBound = fmt.Sprintf("%f", point.ExplicitBounds[i])
			}

			series := newSeries(fmt.Sprintf("%s_bucket", name), point.Attributes)
			series.Labels = append(series.Labels, prompb.Label{
				Name:  []byte("le"),
				Value: []byte(upperBound),
			})
			series.Samples = []prompb.Sample{
				{
					Timestamp: timestamp,
					Value:     float64(point.BucketCounts[i]),
				},
			}
			if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
				return err
			}
		}
	}
	return nil
}

// THIS IMPL IS NOT USED.
// After building this, I discovered that there is not wide support yet for implementations creating ExponentialHistograms.
// There is also a lack of widespread documented examples on how the data looks for these histograms except for trivial cases,
// making testing difficult for negative buckets, etc.
//
//lint:ignore U1000 This is not used, but is kept for future reference.
func (t *OltpMetricWriter) addOltpExpHistogramPoints(ctx context.Context, name string, points []*metricsv1.ExponentialHistogramDataPoint, wr *prompb.WriteRequest) error {
	for _, point := range points {
		timestamp := unixNanoToUnixMillis(point.TimeUnixNano)

		// Add count series
		series := newSeries(fmt.Sprintf("%s_count", name), point.Attributes)
		series.Samples = []prompb.Sample{
			{
				Timestamp: timestamp,
				Value:     float64(point.Count),
			},
		}
		if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
			return err
		}

		// Add sum series, if present
		if point.Sum != nil {
			series := newSeries(fmt.Sprintf("%s_sum", name), point.Attributes)
			series.Samples = []prompb.Sample{
				{
					Timestamp: timestamp,
					Value:     float64(*point.Sum),
				},
			}
			if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
				return err
			}
		}

		// See https://opentelemetry.io/blog/2023/exponential-histograms/
		base := math.Pow(2.0, math.Pow(2.0, float64(point.Scale)))
		if point.Negative != nil {
			buckets := point.Negative
			offset := buckets.Offset
			for i := int32(len(buckets.BucketCounts)) - 1; i >= 0; i-- {
				bucketIdx := i + offset

				// For negative buckets, the "upper bound" is the lower bound of the bucket
				// The lower bound is the higher number.
				upperBound := fmt.Sprintf("%f", math.Pow(-base, float64(bucketIdx)))

				series := newSeries(fmt.Sprintf("%s_bucket", name), point.Attributes)
				series.Labels = append(series.Labels, prompb.Label{
					Name:  []byte("le"),
					Value: []byte(upperBound),
				})
				series.Samples = []prompb.Sample{
					{
						Timestamp: timestamp,
						Value:     float64(buckets.BucketCounts[i]),
					},
				}
				if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
					return err
				}
			}
		}
		// TODO - understand better how to handle point.ZeroCount

		if point.Positive != nil {
			buckets := point.Positive
			offset := buckets.Offset
			for i := int32(0); i < int32(len(buckets.BucketCounts)); i++ {
				bucketIdx := i + offset

				// For positive buckets, the "upper bound" is the higher bound of the bucket
				upperBound := fmt.Sprintf("%f", math.Pow(base, float64(bucketIdx+1)))

				series := newSeries(fmt.Sprintf("%s_bucket", name), point.Attributes)
				series.Labels = append(series.Labels, prompb.Label{
					Name:  []byte("le"),
					Value: []byte(upperBound),
				})
				series.Samples = []prompb.Sample{
					{
						Timestamp: timestamp,
						Value:     float64(buckets.BucketCounts[i]),
					},
				}
				if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (t *OltpMetricWriter) addOltpSummaryPoints(ctx context.Context, name string, points []*metricsv1.SummaryDataPoint, wr *prompb.WriteRequest) error {
	for _, point := range points {
		timestamp := unixNanoToUnixMillis(point.TimeUnixNano)

		// Add count series
		series := newSeries(fmt.Sprintf("%s_count", name), point.Attributes)
		series.Samples = []prompb.Sample{
			{
				Timestamp: timestamp,
				Value:     float64(point.Count),
			},
		}
		if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
			return err
		}

		series = newSeries(fmt.Sprintf("%s_sum", name), point.Attributes)
		series.Samples = []prompb.Sample{
			{
				Timestamp: timestamp,
				Value:     point.Sum,
			},
		}
		if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
			return err
		}

		// Add quantile series
		for _, quantile := range point.QuantileValues {
			series := newSeries(name, point.Attributes)
			series.Labels = append(series.Labels, prompb.Label{
				Name:  []byte("quantile"),
				Value: []byte(fmt.Sprintf("%f", quantile.Quantile)),
			})
			series.Samples = []prompb.Sample{
				{
					Timestamp: timestamp,
					Value:     quantile.Value,
				},
			}
			if err := t.addSeriesAndFlushIfNecessary(ctx, wr, series); err != nil {
				return err
			}
		}
	}
	return nil
}

func newSeries(name string, attributes []*commonv1.KeyValue) prompb.TimeSeries {
	ts := prompb.TimeSeries{
		Labels: make([]prompb.Label, 0, len(attributes)+1),
	}

	ts.Labels = append(ts.Labels, prompb.Label{
		Name:  []byte("__name__"),
		Value: []byte(name),
	})

	for _, l := range attributes {
		ts.Labels = append(ts.Labels, prompb.Label{
			Name:  []byte(l.Key),
			Value: []byte(l.Value.String()),
		})
	}

	return ts
}

func unixNanoToUnixMillis(nano uint64) int64 {
	// Timestamp is UnixMillis
	// This conversion from uint64 to int64 is safe, since math.MaxUint64/1e6 is less than math.MaxInt64.
	return int64(nano / 1e6)
}

func asFloat64(point *metricsv1.NumberDataPoint) float64 {
	switch val := point.Value.(type) {
	case *metricsv1.NumberDataPoint_AsInt:
		// Potentially lossy. Not all int64 values can be represented as float64.
		// Integers between -2^53 and 2^53 are safe, which should be most common in this use case.
		return float64(val.AsInt)
	case *metricsv1.NumberDataPoint_AsDouble:
		return val.AsDouble
	}

	logger.Errorf("unknown number data point type: %T", point.Value)
	return 0.0
}
