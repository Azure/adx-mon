package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/metrics/v1"
	commonv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/common/v1"
	metricsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/metrics/v1"
	resourcev1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/resource/v1"
	"github.com/Azure/adx-mon/collector/export"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/transform"
	"github.com/golang/protobuf/proto"
)

/*
Example OTLP Metric JSON format:

{
  "name": "service_response_time_avg",
  "gauge": {
    "dataPoints": [{
      "value": 245.7,
      "timeUnixNano": "1640995200000000000",
      "attributes": [
        {"key": "ServiceName", "value": "api-gateway"},
        {"key": "Region", "value": "us-east-1"}
      ]
    }]
  }
}

Additional supported metric types:

Gauge:
{
  "name": "cpu_usage",
  "gauge": {
    "dataPoints": [{
      "value": 75.5,
      "timeUnixNano": "1640995200000000000",
      "attributes": [{"key": "host", "value": "server-01"}]
    }]
  }
}

Sum (Counter):
{
  "name": "requests_total",
  "sum": {
    "aggregationTemporality": 2,
    "isMonotonic": true,
    "dataPoints": [{
      "value": 1234,
      "timeUnixNano": "1640995200000000000",
      "attributes": [{"key": "method", "value": "GET"}]
    }]
  }
}

Histogram:
{
  "name": "request_duration_seconds",
  "histogram": {
    "aggregationTemporality": 2,
    "dataPoints": [{
      "count": "100",
      "sum": 45.6,
      "timeUnixNano": "1640995200000000000",
      "bucketCounts": ["10", "20", "30", "40"],
      "explicitBounds": [0.1, 0.5, 1.0, 5.0],
      "attributes": [{"key": "service", "value": "api"}]
    }]
  }
}

Notes:
- timeUnixNano should be a string representing nanoseconds since Unix epoch
- value for gauge/sum can be a number (float64) or string
- count in histogram should be a string
- bucketCounts should be an array of strings
- attributes is an array of {"key": "string", "value": "string"} objects
*/

// MetricJSON represents the input JSON structure for a metric
type MetricJSON struct {
	Name      string         `json:"name"`
	Gauge     *GaugeJSON     `json:"gauge,omitempty"`
	Sum       *SumJSON       `json:"sum,omitempty"`
	Histogram *HistogramJSON `json:"histogram,omitempty"`
	Summary   *SummaryJSON   `json:"summary,omitempty"`
}

type GaugeJSON struct {
	DataPoints []NumberDataPointJSON `json:"dataPoints"`
}

type SumJSON struct {
	AggregationTemporality int                   `json:"aggregationTemporality,omitempty"`
	IsMonotonic            bool                  `json:"isMonotonic,omitempty"`
	DataPoints             []NumberDataPointJSON `json:"dataPoints"`
}

type HistogramJSON struct {
	AggregationTemporality int                      `json:"aggregationTemporality,omitempty"`
	DataPoints             []HistogramDataPointJSON `json:"dataPoints"`
}

type SummaryJSON struct {
	DataPoints []SummaryDataPointJSON `json:"dataPoints"`
}

type NumberDataPointJSON struct {
	Value        interface{}     `json:"value"`
	TimeUnixNano string          `json:"timeUnixNano"`
	Attributes   []AttributeJSON `json:"attributes,omitempty"`
}

type HistogramDataPointJSON struct {
	Count          string          `json:"count"`
	Sum            *float64        `json:"sum,omitempty"`
	TimeUnixNano   string          `json:"timeUnixNano"`
	BucketCounts   []string        `json:"bucketCounts,omitempty"`
	ExplicitBounds []float64       `json:"explicitBounds,omitempty"`
	Attributes     []AttributeJSON `json:"attributes,omitempty"`
}

type SummaryDataPointJSON struct {
	Count        string          `json:"count"`
	Sum          *float64        `json:"sum,omitempty"`
	TimeUnixNano string          `json:"timeUnixNano"`
	Quantiles    []QuantileJSON  `json:"quantiles,omitempty"`
	Attributes   []AttributeJSON `json:"attributes,omitempty"`
}

type QuantileJSON struct {
	Quantile float64 `json:"quantile"`
	Value    float64 `json:"value"`
}

type AttributeJSON struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func main() {
	var (
		inputFile string
		endpoint  string
		timeout   time.Duration
		insecure  bool
		verbose   bool
		dryRun    bool
	)

	flag.StringVar(&inputFile, "input", "", "Path to JSON file containing OTLP metric")
	flag.StringVar(&endpoint, "endpoint", "", "OTLP/HTTP endpoint URL (e.g., https://otlp.example.com/v1/metrics)")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "HTTP timeout")
	flag.BoolVar(&insecure, "insecure-skip-verify", false, "Skip TLS certificate verification")
	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.BoolVar(&dryRun, "dry-run", false, "Validate input and show what would be sent, but don't actually send")

	flag.Parse()

	if inputFile == "" {
		logger.Fatalf("input file is required")
	}

	if endpoint == "" && !dryRun {
		logger.Fatalf("endpoint is required (unless using --dry-run)")
	}

	if verbose {
		logger.Infof("Starting write-metric tool")
		logger.Infof("Input file: %s", inputFile)
		if !dryRun {
			logger.Infof("Endpoint: %s", endpoint)
		}
	}

	// Read and parse input file
	logger.Infof("Reading input file: %s", inputFile)
	data, err := os.ReadFile(inputFile)
	if err != nil {
		logger.Fatalf("Failed to read input file: %v", err)
	}

	var metricJSON MetricJSON
	if err := json.Unmarshal(data, &metricJSON); err != nil {
		logger.Fatalf("Failed to parse JSON: %v", err)
	}

	logger.Infof("Successfully parsed metric: %s", metricJSON.Name)

	// Validate and convert to OTLP
	otlpMetric, err := convertToOTLPMetric(&metricJSON)
	if err != nil {
		logger.Fatalf("Failed to convert to OTLP metric: %v", err)
	}

	// Create OTLP request
	request := &v1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{
			{
				Resource: &resourcev1.Resource{
					Attributes: []*commonv1.KeyValue{},
				},
				ScopeMetrics: []*metricsv1.ScopeMetrics{
					{
						Metrics: []*metricsv1.Metric{otlpMetric},
					},
				},
			},
		},
	}

	logger.Infof("Successfully created OTLP request with metric: %s", otlpMetric.Name)

	if dryRun {
		logger.Infof("Dry run mode - showing what would be sent:")

		// Convert to protobuf and show size
		serialized, err := proto.Marshal(request)
		if err != nil {
			logger.Fatalf("Failed to serialize request: %v", err)
		}

		logger.Infof("Request would be %d bytes", len(serialized))
		logger.Infof("Metric name: %s", otlpMetric.Name)

		// Show data points count based on metric type
		switch data := otlpMetric.Data.(type) {
		case *metricsv1.Metric_Gauge:
			logger.Infof("Gauge with %d data points", len(data.Gauge.DataPoints))
		case *metricsv1.Metric_Sum:
			logger.Infof("Sum with %d data points", len(data.Sum.DataPoints))
		case *metricsv1.Metric_Histogram:
			logger.Infof("Histogram with %d data points", len(data.Histogram.DataPoints))
		case *metricsv1.Metric_Summary:
			logger.Infof("Summary with %d data points", len(data.Summary.DataPoints))
		}

		logger.Infof("Dry run completed successfully")
		return
	}

	// Create exporter and send
	logger.Infof("Sending metric to endpoint: %s", endpoint)

	// Create OTLP exporter using the existing infrastructure
	// Need to import transform package for the RequestTransformer
	transformer := &transform.RequestTransformer{}
	exporter := export.NewPromToOtlpExporter(export.PromToOtlpExporterOpts{
		Destination:        endpoint,
		Timeout:            timeout,
		InsecureSkipVerify: insecure,
		Transformer:        transformer,
	})
	defer exporter.CloseIdleConnections()

	// Convert our OTLP metric to Prometheus format to exercise the full exporter code path
	promWriteRequest, err := convertOTLPToPrometheus(otlpMetric)
	if err != nil {
		logger.Fatalf("Failed to convert to Prometheus format: %v", err)
	}

	// Send using the exporter's Write method (exercises full code path)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = exporter.Write(ctx, promWriteRequest)
	if err != nil {
		logger.Fatalf("Failed to send metric: %v", err)
	}

	logger.Infof("Successfully sent metric to endpoint")
	logger.Infof("Export completed successfully")
}

func convertToOTLPMetric(metricJSON *MetricJSON) (*metricsv1.Metric, error) {
	if metricJSON.Name == "" {
		return nil, fmt.Errorf("metric name is required")
	}

	metric := &metricsv1.Metric{
		Name: metricJSON.Name,
	}

	// Count how many metric types are defined
	typeCount := 0
	if metricJSON.Gauge != nil {
		typeCount++
	}
	if metricJSON.Sum != nil {
		typeCount++
	}
	if metricJSON.Histogram != nil {
		typeCount++
	}
	if metricJSON.Summary != nil {
		typeCount++
	}

	if typeCount == 0 {
		return nil, fmt.Errorf("metric must have one of: gauge, sum, histogram, or summary")
	}
	if typeCount > 1 {
		return nil, fmt.Errorf("metric can only have one type (gauge, sum, histogram, or summary)")
	}

	// Convert based on type
	if metricJSON.Gauge != nil {
		gauge, err := convertGauge(metricJSON.Gauge)
		if err != nil {
			return nil, fmt.Errorf("failed to convert gauge: %w", err)
		}
		metric.Data = &metricsv1.Metric_Gauge{Gauge: gauge}
	} else if metricJSON.Sum != nil {
		sum, err := convertSum(metricJSON.Sum)
		if err != nil {
			return nil, fmt.Errorf("failed to convert sum: %w", err)
		}
		metric.Data = &metricsv1.Metric_Sum{Sum: sum}
	} else if metricJSON.Histogram != nil {
		histogram, err := convertHistogram(metricJSON.Histogram)
		if err != nil {
			return nil, fmt.Errorf("failed to convert histogram: %w", err)
		}
		metric.Data = &metricsv1.Metric_Histogram{Histogram: histogram}
	} else if metricJSON.Summary != nil {
		summary, err := convertSummary(metricJSON.Summary)
		if err != nil {
			return nil, fmt.Errorf("failed to convert summary: %w", err)
		}
		metric.Data = &metricsv1.Metric_Summary{Summary: summary}
	}

	return metric, nil
}

func convertGauge(gaugeJSON *GaugeJSON) (*metricsv1.Gauge, error) {
	if len(gaugeJSON.DataPoints) == 0 {
		return nil, fmt.Errorf("gauge must have at least one data point")
	}

	dataPoints := make([]*metricsv1.NumberDataPoint, len(gaugeJSON.DataPoints))
	for i, dp := range gaugeJSON.DataPoints {
		converted, err := convertNumberDataPoint(&dp)
		if err != nil {
			return nil, fmt.Errorf("failed to convert data point %d: %w", i, err)
		}
		dataPoints[i] = converted
	}

	return &metricsv1.Gauge{
		DataPoints: dataPoints,
	}, nil
}

func convertSum(sumJSON *SumJSON) (*metricsv1.Sum, error) {
	if len(sumJSON.DataPoints) == 0 {
		return nil, fmt.Errorf("sum must have at least one data point")
	}

	dataPoints := make([]*metricsv1.NumberDataPoint, len(sumJSON.DataPoints))
	for i, dp := range sumJSON.DataPoints {
		converted, err := convertNumberDataPoint(&dp)
		if err != nil {
			return nil, fmt.Errorf("failed to convert data point %d: %w", i, err)
		}
		dataPoints[i] = converted
	}

	return &metricsv1.Sum{
		AggregationTemporality: metricsv1.AggregationTemporality(sumJSON.AggregationTemporality),
		IsMonotonic:            sumJSON.IsMonotonic,
		DataPoints:             dataPoints,
	}, nil
}

func convertHistogram(histogramJSON *HistogramJSON) (*metricsv1.Histogram, error) {
	if len(histogramJSON.DataPoints) == 0 {
		return nil, fmt.Errorf("histogram must have at least one data point")
	}

	dataPoints := make([]*metricsv1.HistogramDataPoint, len(histogramJSON.DataPoints))
	for i, dp := range histogramJSON.DataPoints {
		converted, err := convertHistogramDataPoint(&dp)
		if err != nil {
			return nil, fmt.Errorf("failed to convert data point %d: %w", i, err)
		}
		dataPoints[i] = converted
	}

	return &metricsv1.Histogram{
		AggregationTemporality: metricsv1.AggregationTemporality(histogramJSON.AggregationTemporality),
		DataPoints:             dataPoints,
	}, nil
}

func convertSummary(summaryJSON *SummaryJSON) (*metricsv1.Summary, error) {
	if len(summaryJSON.DataPoints) == 0 {
		return nil, fmt.Errorf("summary must have at least one data point")
	}

	dataPoints := make([]*metricsv1.SummaryDataPoint, len(summaryJSON.DataPoints))
	for i, dp := range summaryJSON.DataPoints {
		converted, err := convertSummaryDataPoint(&dp)
		if err != nil {
			return nil, fmt.Errorf("failed to convert data point %d: %w", i, err)
		}
		dataPoints[i] = converted
	}

	return &metricsv1.Summary{
		DataPoints: dataPoints,
	}, nil
}

func convertNumberDataPoint(dpJSON *NumberDataPointJSON) (*metricsv1.NumberDataPoint, error) {
	timeUnixNano, err := parseTimeUnixNano(dpJSON.TimeUnixNano)
	if err != nil {
		return nil, fmt.Errorf("invalid timeUnixNano: %w", err)
	}

	attributes, err := convertAttributes(dpJSON.Attributes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert attributes: %w", err)
	}

	dp := &metricsv1.NumberDataPoint{
		TimeUnixNano: timeUnixNano,
		Attributes:   attributes,
	}

	// Handle value conversion
	switch v := dpJSON.Value.(type) {
	case float64:
		dp.Value = &metricsv1.NumberDataPoint_AsDouble{AsDouble: v}
	case string:
		// Try to parse as int first, then float
		if intVal, err := parseAsInt64(v); err == nil {
			dp.Value = &metricsv1.NumberDataPoint_AsInt{AsInt: intVal}
		} else if floatVal, err := parseAsFloat64(v); err == nil {
			dp.Value = &metricsv1.NumberDataPoint_AsDouble{AsDouble: floatVal}
		} else {
			return nil, fmt.Errorf("value '%s' is not a valid number", v)
		}
	case int:
		dp.Value = &metricsv1.NumberDataPoint_AsInt{AsInt: int64(v)}
	case int64:
		dp.Value = &metricsv1.NumberDataPoint_AsInt{AsInt: v}
	default:
		return nil, fmt.Errorf("value must be a number or string, got %T", v)
	}

	return dp, nil
}

func convertHistogramDataPoint(dpJSON *HistogramDataPointJSON) (*metricsv1.HistogramDataPoint, error) {
	timeUnixNano, err := parseTimeUnixNano(dpJSON.TimeUnixNano)
	if err != nil {
		return nil, fmt.Errorf("invalid timeUnixNano: %w", err)
	}

	count, err := parseAsUint64(dpJSON.Count)
	if err != nil {
		return nil, fmt.Errorf("invalid count: %w", err)
	}

	attributes, err := convertAttributes(dpJSON.Attributes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert attributes: %w", err)
	}

	bucketCounts := make([]uint64, len(dpJSON.BucketCounts))
	for i, bc := range dpJSON.BucketCounts {
		val, err := parseAsUint64(bc)
		if err != nil {
			return nil, fmt.Errorf("invalid bucket count %d: %w", i, err)
		}
		bucketCounts[i] = val
	}

	dp := &metricsv1.HistogramDataPoint{
		TimeUnixNano:   timeUnixNano,
		Count:          count,
		BucketCounts:   bucketCounts,
		ExplicitBounds: dpJSON.ExplicitBounds,
		Attributes:     attributes,
	}

	if dpJSON.Sum != nil {
		dp.Sum = dpJSON.Sum
	}

	return dp, nil
}

func convertSummaryDataPoint(dpJSON *SummaryDataPointJSON) (*metricsv1.SummaryDataPoint, error) {
	timeUnixNano, err := parseTimeUnixNano(dpJSON.TimeUnixNano)
	if err != nil {
		return nil, fmt.Errorf("invalid timeUnixNano: %w", err)
	}

	count, err := parseAsUint64(dpJSON.Count)
	if err != nil {
		return nil, fmt.Errorf("invalid count: %w", err)
	}

	attributes, err := convertAttributes(dpJSON.Attributes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert attributes: %w", err)
	}

	quantiles := make([]*metricsv1.SummaryDataPoint_ValueAtQuantile, len(dpJSON.Quantiles))
	for i, q := range dpJSON.Quantiles {
		quantiles[i] = &metricsv1.SummaryDataPoint_ValueAtQuantile{
			Quantile: q.Quantile,
			Value:    q.Value,
		}
	}

	dp := &metricsv1.SummaryDataPoint{
		TimeUnixNano:   timeUnixNano,
		Count:          count,
		QuantileValues: quantiles,
		Attributes:     attributes,
	}

	if dpJSON.Sum != nil {
		dp.Sum = *dpJSON.Sum
	}

	return dp, nil
}

func convertAttributes(attrsJSON []AttributeJSON) ([]*commonv1.KeyValue, error) {
	attributes := make([]*commonv1.KeyValue, len(attrsJSON))
	for i, attr := range attrsJSON {
		if attr.Key == "" {
			return nil, fmt.Errorf("attribute %d: key cannot be empty", i)
		}
		attributes[i] = &commonv1.KeyValue{
			Key: attr.Key,
			Value: &commonv1.AnyValue{
				Value: &commonv1.AnyValue_StringValue{StringValue: attr.Value},
			},
		}
	}
	return attributes, nil
}

// Helper functions for parsing
func parseTimeUnixNano(s string) (uint64, error) {
	return parseAsUint64(s)
}

func parseAsUint64(s string) (uint64, error) {
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse '%s' as uint64: %w", s, err)
	}
	return val, nil
}

func parseAsInt64(s string) (int64, error) {
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse '%s' as int64: %w", s, err)
	}
	return val, nil
}

func parseAsFloat64(s string) (float64, error) {
	var val float64
	if _, err := fmt.Sscanf(s, "%f", &val); err != nil {
		return 0, fmt.Errorf("failed to parse '%s' as float64: %w", s, err)
	}
	return val, nil
}

// convertOTLPToPrometheus converts an OTLP metric to Prometheus WriteRequest format
// This allows us to exercise the full exporter code path
func convertOTLPToPrometheus(metric *metricsv1.Metric) (*prompb.WriteRequest, error) {
	writeRequest := &prompb.WriteRequest{
		Timeseries: []*prompb.TimeSeries{},
	}

	// Convert based on metric type
	switch data := metric.Data.(type) {
	case *metricsv1.Metric_Gauge:
		for _, dp := range data.Gauge.DataPoints {
			ts := &prompb.TimeSeries{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte(metric.Name)},
				},
				Samples: []*prompb.Sample{},
			}

			// Add attributes as labels
			for _, attr := range dp.Attributes {
				ts.Labels = append(ts.Labels, &prompb.Label{
					Name:  []byte(attr.Key),
					Value: []byte(attr.Value.GetStringValue()),
				})
			}

			// Convert value
			var value float64
			switch v := dp.Value.(type) {
			case *metricsv1.NumberDataPoint_AsDouble:
				value = v.AsDouble
			case *metricsv1.NumberDataPoint_AsInt:
				value = float64(v.AsInt)
			default:
				return nil, fmt.Errorf("unsupported value type in gauge data point")
			}

			ts.Samples = append(ts.Samples, &prompb.Sample{
				Value:     value,
				Timestamp: int64(dp.TimeUnixNano / 1000000), // nanoseconds to milliseconds
			})

			writeRequest.Timeseries = append(writeRequest.Timeseries, ts)
		}

	case *metricsv1.Metric_Sum:
		for _, dp := range data.Sum.DataPoints {
			ts := &prompb.TimeSeries{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte(metric.Name)},
				},
				Samples: []*prompb.Sample{},
			}

			// Add attributes as labels
			for _, attr := range dp.Attributes {
				ts.Labels = append(ts.Labels, &prompb.Label{
					Name:  []byte(attr.Key),
					Value: []byte(attr.Value.GetStringValue()),
				})
			}

			// Convert value
			var value float64
			switch v := dp.Value.(type) {
			case *metricsv1.NumberDataPoint_AsDouble:
				value = v.AsDouble
			case *metricsv1.NumberDataPoint_AsInt:
				value = float64(v.AsInt)
			default:
				return nil, fmt.Errorf("unsupported value type in sum data point")
			}

			ts.Samples = append(ts.Samples, &prompb.Sample{
				Value:     value,
				Timestamp: int64(dp.TimeUnixNano / 1000000), // nanoseconds to milliseconds
			})

			writeRequest.Timeseries = append(writeRequest.Timeseries, ts)
		}

	case *metricsv1.Metric_Histogram:
		// For histogram, we'll create a simplified representation
		// In practice, histograms are more complex in Prometheus
		for _, dp := range data.Histogram.DataPoints {
			// Create a count series
			countTs := &prompb.TimeSeries{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte(metric.Name + "_count")},
				},
				Samples: []*prompb.Sample{
					{
						Value:     float64(dp.Count),
						Timestamp: int64(dp.TimeUnixNano / 1000000),
					},
				},
			}

			// Add attributes as labels
			for _, attr := range dp.Attributes {
				countTs.Labels = append(countTs.Labels, &prompb.Label{
					Name:  []byte(attr.Key),
					Value: []byte(attr.Value.GetStringValue()),
				})
			}

			writeRequest.Timeseries = append(writeRequest.Timeseries, countTs)

			// Create a sum series if available
			if dp.Sum != nil {
				sumTs := &prompb.TimeSeries{
					Labels: []*prompb.Label{
						{Name: []byte("__name__"), Value: []byte(metric.Name + "_sum")},
					},
					Samples: []*prompb.Sample{
						{
							Value:     *dp.Sum,
							Timestamp: int64(dp.TimeUnixNano / 1000000),
						},
					},
				}

				// Add attributes as labels
				for _, attr := range dp.Attributes {
					sumTs.Labels = append(sumTs.Labels, &prompb.Label{
						Name:  []byte(attr.Key),
						Value: []byte(attr.Value.GetStringValue()),
					})
				}

				writeRequest.Timeseries = append(writeRequest.Timeseries, sumTs)
			}
		}

	case *metricsv1.Metric_Summary:
		// For summary, create count and sum series
		for _, dp := range data.Summary.DataPoints {
			// Create a count series
			countTs := &prompb.TimeSeries{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte(metric.Name + "_count")},
				},
				Samples: []*prompb.Sample{
					{
						Value:     float64(dp.Count),
						Timestamp: int64(dp.TimeUnixNano / 1000000),
					},
				},
			}

			// Add attributes as labels
			for _, attr := range dp.Attributes {
				countTs.Labels = append(countTs.Labels, &prompb.Label{
					Name:  []byte(attr.Key),
					Value: []byte(attr.Value.GetStringValue()),
				})
			}

			writeRequest.Timeseries = append(writeRequest.Timeseries, countTs)

			// Create a sum series
			sumTs := &prompb.TimeSeries{
				Labels: []*prompb.Label{
					{Name: []byte("__name__"), Value: []byte(metric.Name + "_sum")},
				},
				Samples: []*prompb.Sample{
					{
						Value:     dp.Sum,
						Timestamp: int64(dp.TimeUnixNano / 1000000),
					},
				},
			}

			// Add attributes as labels
			for _, attr := range dp.Attributes {
				sumTs.Labels = append(sumTs.Labels, &prompb.Label{
					Name:  []byte(attr.Key),
					Value: []byte(attr.Value.GetStringValue()),
				})
			}

			writeRequest.Timeseries = append(writeRequest.Timeseries, sumTs)
		}

	default:
		return nil, fmt.Errorf("unsupported metric type")
	}

	return writeRequest, nil
}
