package transform

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestNewKustoToPrometheusTransformer(t *testing.T) {
	config := TransformConfig{
		ValueColumn: "value",
	}
	meter := noop.NewMeterProvider().Meter("test")

	transformer := NewKustoToMetricsTransformer(config, meter)

	require.Equal(t, "value", transformer.config.ValueColumn)
	require.NotNil(t, transformer.meter)
}

func TestTransformBasic(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	results := []map[string]any{
		{
			"value": 42.0,
		},
	}

	metrics, err := transformer.Transform(results)
	require.NoError(t, err)
	require.Len(t, metrics, 1)

	metric := metrics[0]
	require.Equal(t, "test_metric", metric.Name)
	require.Equal(t, 42.0, metric.Value)
}

func TestTransformWithMetricNameColumn(t *testing.T) {
	config := TransformConfig{
		MetricNameColumn: "metric_name",
		ValueColumn:      "value",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	results := []map[string]any{
		{
			"metric_name": "cpu_usage",
			"value":       85.5,
		},
	}

	metrics, err := transformer.Transform(results)
	require.NoError(t, err)
	require.Len(t, metrics, 1)

	metric := metrics[0]
	require.Equal(t, "cpu_usage", metric.Name)
	require.Equal(t, 85.5, metric.Value)
}

func TestTransformWithLabels(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
		LabelColumns:      []string{"host", "service"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	results := []map[string]any{
		{
			"value":   100.0,
			"host":    "server1",
			"service": "api",
		},
	}

	metrics, err := transformer.Transform(results)
	require.NoError(t, err)
	require.Len(t, metrics, 1)

	metric := metrics[0]
	require.Len(t, metric.Labels, 2)
	require.Equal(t, "server1", metric.Labels["host"])
	require.Equal(t, "api", metric.Labels["service"])
}

func TestTransformWithTimestamp(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
		TimestampColumn:   "timestamp",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	testTime := time.Date(2023, 12, 25, 12, 0, 0, 0, time.UTC)
	results := []map[string]any{
		{
			"value":     50.0,
			"timestamp": testTime,
		},
	}

	metrics, err := transformer.Transform(results)
	require.NoError(t, err)
	require.Len(t, metrics, 1)

	metric := metrics[0]
	require.True(t, metric.Timestamp.Equal(testTime))
}

func TestTransformValueTypes(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	testCases := []struct {
		name     string
		value    any
		expected float64
	}{
		{"float64", 42.5, 42.5},
		{"float32", float32(42.5), 42.5},
		{"int", 42, 42.0},
		{"int32", int32(42), 42.0},
		{"int64", int64(42), 42.0},
		{"string", "42.5", 42.5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := []map[string]any{
				{"value": tc.value},
			}

			metrics, err := transformer.Transform(results)
			require.NoError(t, err)
			require.Len(t, metrics, 1)
			require.Equal(t, tc.expected, metrics[0].Value)
		})
	}
}

func TestTransformKustoValueTypes(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	testCases := []struct {
		name     string
		value    any
		expected float64
	}{
		{"value.Long valid", value.Long{Value: 42, Valid: true}, 42.0},
		{"value.Real valid", value.Real{Value: 42.5, Valid: true}, 42.5},
		{"value.Int valid", value.Int{Value: 42, Valid: true}, 42.0},
		{"value.Long invalid", value.Long{Value: 42, Valid: false}, 0.0},   // Should fail validation
		{"value.Real invalid", value.Real{Value: 42.5, Valid: false}, 0.0}, // Should fail validation
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := []map[string]any{
				{"value": tc.value},
			}

			// Test validation first
			err := transformer.Validate(results)
			if tc.name == "value.Long invalid" || tc.name == "value.Real invalid" {
				// These should fail validation due to Valid=false
				require.Error(t, err)
				require.Contains(t, err.Error(), "null value")
				return
			}
			require.NoError(t, err, "Validation should pass for valid Kusto types")

			// Test transformation
			metrics, err := transformer.Transform(results)
			require.NoError(t, err, "Transform should handle Kusto types")
			require.Len(t, metrics, 1)
			require.Equal(t, tc.expected, metrics[0].Value)
		})
	}
}

func TestTransformTimestampTypes(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
		TimestampColumn:   "timestamp",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	testTime := time.Date(2023, 12, 25, 12, 0, 0, 0, time.UTC)
	testCases := []struct {
		name      string
		timestamp any
		expected  time.Time
	}{
		{"time.Time", testTime, testTime},
		{"RFC3339", "2023-12-25T12:00:00Z", testTime},
		{"RFC3339Nano", "2023-12-25T12:00:00.000000000Z", testTime},
		{"custom format", "2023-12-25 12:00:00", testTime},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := []map[string]any{
				{
					"value":     42.0,
					"timestamp": tc.timestamp,
				},
			}

			metrics, err := transformer.Transform(results)
			require.NoError(t, err)
			require.Len(t, metrics, 1)
			require.True(t, metrics[0].Timestamp.Equal(tc.expected))
		})
	}
}

func TestTransformErrors(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")

	testCases := []struct {
		name    string
		config  TransformConfig
		results []map[string]any
		wantErr bool
	}{
		{
			name: "missing value column",
			config: TransformConfig{
				ValueColumn:       "missing",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
		},
		{
			name: "null value",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": nil},
			},
			wantErr: true,
		},
		{
			name: "no metric name config",
			config: TransformConfig{
				ValueColumn: "value",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
		},
		{
			name: "missing metric name column",
			config: TransformConfig{
				ValueColumn:      "value",
				MetricNameColumn: "missing",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
		},
		{
			name: "empty metric name",
			config: TransformConfig{
				ValueColumn:      "value",
				MetricNameColumn: "name",
			},
			results: []map[string]any{
				{"value": 42.0, "name": ""},
			},
			wantErr: true,
		},
		{
			name: "invalid value type",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": []string{"not", "a", "number"}},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transformer := NewKustoToMetricsTransformer(tc.config, meter)
			_, err := transformer.Transform(tc.results)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")

	testCases := []struct {
		name    string
		config  TransformConfig
		results []map[string]any
		wantErr bool
	}{
		{
			name: "valid config",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: false,
		},
		{
			name: "empty results",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{},
			wantErr: false,
		},
		{
			name: "missing value column",
			config: TransformConfig{
				ValueColumn:       "missing",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
		},
		{
			name: "null value",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": nil},
			},
			wantErr: true,
		},
		{
			name: "no metric name config",
			config: TransformConfig{
				ValueColumn: "value",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transformer := NewKustoToMetricsTransformer(tc.config, meter)
			err := transformer.Validate(tc.results)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegisterMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(TransformConfig{}, meter)

	metrics := []MetricData{
		{
			Name:      "cpu_usage",
			Value:     85.5,
			Timestamp: time.Now(),
			Labels:    map[string]string{"host": "server1"},
		},
		{
			Name:      "memory_usage",
			Value:     60.2,
			Timestamp: time.Now(),
			Labels:    map[string]string{"host": "server2"},
		},
	}

	ctx := context.Background()
	err := transformer.RegisterMetrics(ctx, metrics)
	require.NoError(t, err)
}

func TestTransformMultipleRows(t *testing.T) {
	config := TransformConfig{
		MetricNameColumn: "metric",
		ValueColumn:      "value",
		LabelColumns:     []string{"host"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	results := []map[string]any{
		{
			"metric": "cpu_usage",
			"value":  85.5,
			"host":   "server1",
		},
		{
			"metric": "memory_usage",
			"value":  60.2,
			"host":   "server1",
		},
		{
			"metric": "cpu_usage",
			"value":  92.1,
			"host":   "server2",
		},
	}

	metrics, err := transformer.Transform(results)
	require.NoError(t, err)
	require.Len(t, metrics, 3)

	// Verify first metric
	require.Equal(t, "cpu_usage", metrics[0].Name)
	require.Equal(t, 85.5, metrics[0].Value)
	require.Equal(t, "server1", metrics[0].Labels["host"])

	// Verify second metric
	require.Equal(t, "memory_usage", metrics[1].Name)
	require.Equal(t, 60.2, metrics[1].Value)
	require.Equal(t, "server1", metrics[1].Labels["host"])

	// Verify third metric
	require.Equal(t, "cpu_usage", metrics[2].Name)
	require.Equal(t, 92.1, metrics[2].Value)
	require.Equal(t, "server2", metrics[2].Labels["host"])
}

func TestValidateKustoValueTypes(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")

	testCases := []struct {
		name    string
		config  TransformConfig
		results []map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name: "value.Long valid should pass validation",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": value.Long{Value: 42, Valid: true}},
			},
			wantErr: false,
		},
		{
			name: "value.Real valid should pass validation",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": value.Real{Value: 42.5, Valid: true}},
			},
			wantErr: false,
		},
		{
			name: "value.Int valid should pass validation",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": value.Int{Value: 42, Valid: true}},
			},
			wantErr: false,
		},
		{
			name: "value.Long invalid should fail validation",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": value.Long{Value: 42, Valid: false}},
			},
			wantErr: true,
			errMsg:  "null value",
		},
		{
			name: "value.Real invalid should fail validation",
			config: TransformConfig{
				ValueColumn:       "value",
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": value.Real{Value: 42.5, Valid: false}},
			},
			wantErr: true,
			errMsg:  "null value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transformer := NewKustoToMetricsTransformer(tc.config, meter)
			err := transformer.Validate(tc.results)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
