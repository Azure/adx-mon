package export

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
	"github.com/Azure/adx-mon/metrics"
	adxhttp "github.com/Azure/adx-mon/pkg/http"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/transform"
	"github.com/golang/protobuf/proto"
)

var (
	nameLabel = []byte("__name__")
)

// PromToOtlpExporter clones Prometheus WriteRequests to an OTLP/HTTP endpoint
// Implements RemoteWriteClient
type PromToOtlpExporter struct {
	httpClient         *http.Client
	destination        string
	transformer        *transform.RequestTransformer
	resourceAttributes []*commonv1.KeyValue

	stringKVPool  *pool.Generic
	datapointPool *pool.Generic
}

type PromToOtlpExporterOpts struct {
	Transformer *transform.RequestTransformer

	Destination string

	AddResourceAttributes map[string]string

	// Close controls whether the client closes the connection after each request.
	Close bool

	// Timeout is the timeout for the http client and the http request.
	Timeout time.Duration

	// InsecureSkipVerify controls whether the client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// IdleConnTimeout is the maximum amount of time an idle (keep-alive) connection
	// will remain idle before closing itself.
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the amount of time to wait for a server's response headers
	// after fully writing the request (including its body, if any).
	ResponseHeaderTimeout time.Duration

	// MaxIdleConns controls the maximum number of idle (keep-alive) connections across all hosts.
	MaxIdleConns int

	// MaxIdleConnsPerHost, if non-zero, controls the maximum idle (keep-alive) per host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost, if non-zero, controls the maximum connections per host.
	MaxConnsPerHost int

	// TLSHandshakeTimeout specifies the maximum amount of time to
	// wait for a TLS handshake. Zero means no timeout.
	TLSHandshakeTimeout time.Duration

	// DisableHTTP2 controls whether the client disables HTTP/2 support.
	DisableHTTP2 bool

	// DisableKeepAlives controls whether the client disables HTTP keep-alives.
	DisableKeepAlives bool
}

func (c PromToOtlpExporterOpts) WithDefaults() PromToOtlpExporterOpts {
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.IdleConnTimeout == 0 {
		c.IdleConnTimeout = 1 * time.Minute
	}
	if c.ResponseHeaderTimeout == 0 {
		c.ResponseHeaderTimeout = 10 * time.Second
	}

	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 100
	}

	if c.MaxIdleConnsPerHost == 0 {
		c.MaxIdleConnsPerHost = 5
	}

	if c.MaxConnsPerHost == 0 {
		c.MaxConnsPerHost = 5
	}

	if c.TLSHandshakeTimeout == 0 {
		c.TLSHandshakeTimeout = 10 * time.Second
	}

	return c
}

func NewPromToOtlpExporter(opts PromToOtlpExporterOpts) *PromToOtlpExporter {
	opts = opts.WithDefaults()
	client := adxhttp.NewClient(
		adxhttp.ClientOpts{
			Timeout:               opts.Timeout,
			InsecureSkipVerify:    opts.InsecureSkipVerify,
			Close:                 opts.Close,
			MaxIdleConnsPerHost:   opts.MaxIdleConnsPerHost,
			MaxIdleConns:          opts.MaxIdleConns,
			IdleConnTimeout:       opts.IdleConnTimeout,
			ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
			DisableHTTP2:          opts.DisableHTTP2,
			DisableKeepAlives:     opts.DisableKeepAlives,
		},
	)

	var attributes []*commonv1.KeyValue
	if opts.AddResourceAttributes != nil {
		attributes = make([]*commonv1.KeyValue, 0, len(opts.AddResourceAttributes))
		for k, v := range opts.AddResourceAttributes {
			attributes = append(attributes, &commonv1.KeyValue{
				Key:   k,
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: v}},
			})
		}
	}
	return &PromToOtlpExporter{
		httpClient:         client,
		destination:        opts.Destination,
		transformer:        opts.Transformer,
		resourceAttributes: attributes,

		stringKVPool: pool.NewGeneric(4096, func(sz int) interface{} {
			return &commonv1.KeyValue{
				Key:   "",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: ""}},
			}
		}),

		datapointPool: pool.NewGeneric(4096, func(sz int) interface{} {
			return &metricsv1.NumberDataPoint{
				Attributes:   nil,
				TimeUnixNano: 0,
				Value:        &metricsv1.NumberDataPoint_AsDouble{AsDouble: 0},
			}
		}),
	}
}

// Write sends the given WriteRequest to the given OTLP endpoint
func (c *PromToOtlpExporter) Write(ctx context.Context, wr *prompb.WriteRequest) error {
	serialized, timeseriesCount, err := c.promToOtlpRequest(wr)
	if err != nil {
		return fmt.Errorf("metric otlp forwarder convert: %w", err)
	}

	if timeseriesCount == 0 {
		return nil
	}
	return c.sendRequest(serialized, timeseriesCount)
}

func (c *PromToOtlpExporter) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

func (c *PromToOtlpExporter) promToOtlpRequest(wr *prompb.WriteRequest) ([]byte, int64, error) {
	scopeMetrics := &metricsv1.ScopeMetrics{
		// TODO - assume we filter most or keep most?
		Metrics: make([]*metricsv1.Metric, 0, len(wr.Timeseries)),
	}
	exportRequest := &v1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{
			{
				Resource: &resourcev1.Resource{
					Attributes: c.resourceAttributes,
				},
				ScopeMetrics: []*metricsv1.ScopeMetrics{
					scopeMetrics,
				},
			},
		},
	}

	count := int64(0)
	for _, ts := range wr.Timeseries {
		nameBytes := prompb.MetricName(ts)
		if c.transformer.ShouldDropMetric(ts, nameBytes) {
			continue
		}
		count++

		gauge := &metricsv1.Gauge{
			DataPoints: make([]*metricsv1.NumberDataPoint, 0, len(ts.Samples)),
		}
		metric := &metricsv1.Metric{
			Data: &metricsv1.Metric_Gauge{
				Gauge: gauge,
			},
		}

		attributes := make([]*commonv1.KeyValue, 0, len(ts.Labels))
		c.transformer.WalkLabels(ts, func(k, v []byte) {
			// skip adding the name label and any adxmon_ prefixed labels
			if bytes.Equal(k, nameLabel) || bytes.HasPrefix(k, []byte("adxmon_")) {
				return
			}
			attribute := c.stringKVPool.Get(0).(*commonv1.KeyValue)
			// Explicitly set all fields to reset, but also to avoid allocations for the value
			attribute.Key = string(k)
			// only accept stringval here to avoid allocations
			stringval := attribute.Value.Value.(*commonv1.AnyValue_StringValue)
			stringval.StringValue = string(v)
			attributes = append(attributes, attribute)
		})
		metric.Name = string(nameBytes)

		for _, sample := range ts.Samples {
			datapoint := c.datapointPool.Get(0).(*metricsv1.NumberDataPoint)
			// Explicitly set all fields to reset, but also to avoid allocations for the value
			datapoint.Attributes = attributes
			datapoint.TimeUnixNano = uint64(sample.Timestamp * 1000000) // milliseconds to nanoseconds
			datapoint.StartTimeUnixNano = 0
			datapoint.Exemplars = nil
			datapoint.Flags = 0
			// only accept doubleval here to avoid allocations
			doubleVal := datapoint.Value.(*metricsv1.NumberDataPoint_AsDouble)
			doubleVal.AsDouble = sample.Value
			gauge.DataPoints = append(gauge.DataPoints, datapoint)
		}

		scopeMetrics.Metrics = append(scopeMetrics.Metrics, metric)
	}

	serialized, err := proto.Marshal(exportRequest)

	for _, scopeMetric := range scopeMetrics.Metrics {
		for idx, datapoint := range scopeMetric.GetGauge().GetDataPoints() {
			if idx == 0 {
				// shared attribute with all datapoints under metric
				for _, attribute := range datapoint.Attributes {
					c.stringKVPool.Put(attribute)
				}
			}

			c.datapointPool.Put(datapoint)
		}
	}

	return serialized, count, err
}

func (c *PromToOtlpExporter) sendRequest(body []byte, count int64) error {
	req, err := http.NewRequest("POST", c.destination, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("metric otlp forwarder request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metric otlp forwarder post: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var exportMetricsServiceResponse v1.ExportMetricsServiceResponse
		var buf []byte
		buf, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return fmt.Errorf("metric otlp forwarder read response: %w", err)
		}
		err = proto.Unmarshal(buf, &exportMetricsServiceResponse)
		if err != nil {
			return fmt.Errorf("metric otlp forwarder unmarshal response: %w", err)
		}

		if exportMetricsServiceResponse.HasPartialSuccess() {
			rejected := exportMetricsServiceResponse.PartialSuccess.RejectedDataPoints
			metrics.CollectorExporterFailed.WithLabelValues("PromToOtlp", c.destination).Add(float64(rejected))
			metrics.CollectorExporterSent.WithLabelValues("PromToOtlp", c.destination).Add(float64(count - rejected))
		} else {
			metrics.CollectorExporterSent.WithLabelValues("PromToOtlp", c.destination).Add(float64(count))
		}
	} else {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return fmt.Errorf("metric otlp forwarder post error code: %s", resp.Status)
	}

	return nil
}
