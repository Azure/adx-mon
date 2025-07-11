package transform

import (
	"context"
	"strings"
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
		errMsg  string
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
			errMsg:  "value column 'missing' not found in query results",
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
			errMsg:  "value column 'value' contains null value",
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
			errMsg:  "no metric name configuration",
		},
		{
			name: "valid multi-value config",
			config: TransformConfig{
				ValueColumns:      []string{"value1", "value2"},
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value1": 42.0, "value2": 24.0},
			},
			wantErr: false,
		},
		{
			name: "multi-value config with missing column",
			config: TransformConfig{
				ValueColumns:      []string{"value1", "missing"},
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value1": 42.0},
			},
			wantErr: true,
			errMsg:  "invalid value column \"missing\": value column 'missing' not found in query results",
		},
		{
			name: "multi-value config with null value",
			config: TransformConfig{
				ValueColumns:      []string{"value1", "value2"},
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value1": 42.0, "value2": nil},
			},
			wantErr: true,
			errMsg:  "invalid value column \"value2\": value column 'value2' contains null value",
		},
		{
			name: "multi-value config with invalid prefix",
			config: TransformConfig{
				ValueColumns:      []string{"value1"},
				DefaultMetricName: "test",
				MetricNamePrefix:  "---",
			},
			results: []map[string]any{
				{"value1": 42.0},
			},
			wantErr: true,
			errMsg:  "MetricNamePrefix \"---\" results in invalid normalized name \"metric\"",
		},
		{
			name: "empty ValueColumns and ValueColumn",
			config: TransformConfig{
				DefaultMetricName: "test",
			},
			results: []map[string]any{
				{"value": 42.0},
			},
			wantErr: true,
			errMsg:  "either ValueColumn or ValueColumns must be specified",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transformer := NewKustoToMetricsTransformer(tc.config, meter)
			err := transformer.Validate(tc.results)

			if tc.wantErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
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

func TestTransformMultiValueColumns(t *testing.T) {
	tests := []struct {
		name     string
		config   TransformConfig
		results  []map[string]any
		expected []MetricData
	}{
		{
			name: "multi-value columns with prefix",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				MetricNamePrefix: "teama",
				ValueColumns:     []string{"Numerator", "Denominator", "AvgLatency"},
				TimestampColumn:  "timestamp",
				LabelColumns:     []string{"LocationId", "ServiceTier"},
			},
			results: []map[string]any{
				{
					"metric_name": "customer_success_rate",
					"Numerator":   1974.0,
					"Denominator": 2000.0,
					"AvgLatency":  150.2,
					"LocationId":  "datacenter-01",
					"ServiceTier": "premium",
					"timestamp":   time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
				},
			},
			expected: []MetricData{
				{
					Name:      "teama_customer_success_rate_numerator",
					Value:     1974.0,
					Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					Labels:    map[string]string{"LocationId": "datacenter-01", "ServiceTier": "premium"},
				},
				{
					Name:      "teama_customer_success_rate_denominator",
					Value:     2000.0,
					Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					Labels:    map[string]string{"LocationId": "datacenter-01", "ServiceTier": "premium"},
				},
				{
					Name:      "teama_customer_success_rate_avg_latency",
					Value:     150.2,
					Timestamp: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
					Labels:    map[string]string{"LocationId": "datacenter-01", "ServiceTier": "premium"},
				},
			},
		},
		{
			name: "multi-value columns without prefix",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				ValueColumns:     []string{"Count", "ErrorRate"},
				LabelColumns:     []string{"service", "region"},
			},
			results: []map[string]any{
				{
					"metric_name": "service_metrics",
					"Count":       1000.0,
					"ErrorRate":   0.05,
					"service":     "api",
					"region":      "us-east",
				},
			},
			expected: []MetricData{
				{
					Name:   "service_metrics_count",
					Value:  1000.0,
					Labels: map[string]string{"service": "api", "region": "us-east"},
				},
				{
					Name:   "service_metrics_error_rate",
					Value:  0.05,
					Labels: map[string]string{"service": "api", "region": "us-east"},
				},
			},
		},
		{
			name: "multi-value columns with default metric name",
			config: TransformConfig{
				DefaultMetricName: "default_metric",
				ValueColumns:      []string{"Value1", "Value2"},
				LabelColumns:      []string{"label1"},
			},
			results: []map[string]any{
				{
					"Value1": 100.0,
					"Value2": 200.0,
					"label1": "test",
				},
			},
			expected: []MetricData{
				{
					Name:   "default_metric_value1",
					Value:  100.0,
					Labels: map[string]string{"label1": "test"},
				},
				{
					Name:   "default_metric_value2",
					Value:  200.0,
					Labels: map[string]string{"label1": "test"},
				},
			},
		},
		{
			name: "multi-value columns multiple rows",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				ValueColumns:     []string{"Requests", "Errors"},
				LabelColumns:     []string{"host"},
			},
			results: []map[string]any{
				{
					"metric_name": "app_metrics",
					"Requests":    1000.0,
					"Errors":      10.0,
					"host":        "server1",
				},
				{
					"metric_name": "app_metrics",
					"Requests":    1500.0,
					"Errors":      5.0,
					"host":        "server2",
				},
			},
			expected: []MetricData{
				{
					Name:   "app_metrics_requests",
					Value:  1000.0,
					Labels: map[string]string{"host": "server1"},
				},
				{
					Name:   "app_metrics_errors",
					Value:  10.0,
					Labels: map[string]string{"host": "server1"},
				},
				{
					Name:   "app_metrics_requests",
					Value:  1500.0,
					Labels: map[string]string{"host": "server2"},
				},
				{
					Name:   "app_metrics_errors",
					Value:  5.0,
					Labels: map[string]string{"host": "server2"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter := noop.NewMeterProvider().Meter("test")
			transformer := NewKustoToMetricsTransformer(tt.config, meter)

			metrics, err := transformer.Transform(tt.results)
			require.NoError(t, err)
			require.Len(t, metrics, len(tt.expected))

			// Sort both slices by metric name for consistent comparison
			sortMetricsByName := func(metrics []MetricData) {
				for i := 0; i < len(metrics)-1; i++ {
					for j := i + 1; j < len(metrics); j++ {
						if metrics[i].Name > metrics[j].Name {
							metrics[i], metrics[j] = metrics[j], metrics[i]
						}
					}
				}
			}

			sortMetricsByName(metrics)
			sortMetricsByName(tt.expected)

			for i, expected := range tt.expected {
				actual := metrics[i]
				require.Equal(t, expected.Name, actual.Name, "Metric name mismatch at index %d", i)
				require.Equal(t, expected.Value, actual.Value, "Metric value mismatch at index %d", i)
				require.Equal(t, expected.Labels, actual.Labels, "Metric labels mismatch at index %d", i)

				// Check timestamp if expected is set
				if !expected.Timestamp.IsZero() {
					require.Equal(t, expected.Timestamp, actual.Timestamp, "Metric timestamp mismatch at index %d", i)
				}
			}
		})
	}
}

func TestTransformMultiValueErrors(t *testing.T) {
	tests := []struct {
		name    string
		config  TransformConfig
		results []map[string]any
		wantErr string
	}{
		{
			name: "missing value column in multi-value mode",
			config: TransformConfig{
				DefaultMetricName: "test_metric",
				ValueColumns:      []string{"ExistingColumn", "MissingColumn"},
			},
			results: []map[string]any{
				{
					"ExistingColumn": 100.0,
					// MissingColumn is not present
				},
			},
			wantErr: "failed to extract values: failed to extract value from column 'MissingColumn': value column 'MissingColumn' not found in row",
		},
		{
			name: "null value in multi-value column",
			config: TransformConfig{
				DefaultMetricName: "test_metric",
				ValueColumns:      []string{"ValidColumn", "NullColumn"},
			},
			results: []map[string]any{
				{
					"ValidColumn": 100.0,
					"NullColumn":  nil,
				},
			},
			wantErr: "failed to extract values: failed to extract value from column 'NullColumn': value column 'NullColumn' contains null value",
		},
		{
			name: "invalid type in multi-value column",
			config: TransformConfig{
				DefaultMetricName: "test_metric",
				ValueColumns:      []string{"ValidColumn", "InvalidColumn"},
			},
			results: []map[string]any{
				{
					"ValidColumn":   100.0,
					"InvalidColumn": map[string]any{"nested": "object"},
				},
			},
			wantErr: "failed to extract values: failed to extract value from column 'InvalidColumn': value column 'InvalidColumn' contains unsupported type map[string]interface {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter := noop.NewMeterProvider().Meter("test")
			transformer := NewKustoToMetricsTransformer(tt.config, meter)

			_, err := transformer.Transform(tt.results)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTransformBackwardCompatibility(t *testing.T) {
	t.Run("single value mode still works", func(t *testing.T) {
		config := TransformConfig{
			DefaultMetricName: "legacy_metric",
			ValueColumn:       "value", // Legacy single-value mode
			LabelColumns:      []string{"host"},
		}
		meter := noop.NewMeterProvider().Meter("test")
		transformer := NewKustoToMetricsTransformer(config, meter)

		results := []map[string]any{
			{
				"value": 42.0,
				"host":  "server1",
			},
		}

		metrics, err := transformer.Transform(results)
		require.NoError(t, err)
		require.Len(t, metrics, 1)

		metric := metrics[0]
		require.Equal(t, "legacy_metric", metric.Name)
		require.Equal(t, 42.0, metric.Value)
		require.Equal(t, map[string]string{"host": "server1"}, metric.Labels)
	})

	t.Run("single value mode with prefix", func(t *testing.T) {
		config := TransformConfig{
			DefaultMetricName: "legacy_metric",
			MetricNamePrefix:  "team",
			ValueColumn:       "value", // Legacy single-value mode
			LabelColumns:      []string{"host"},
		}
		meter := noop.NewMeterProvider().Meter("test")
		transformer := NewKustoToMetricsTransformer(config, meter)

		results := []map[string]any{
			{
				"value": 42.0,
				"host":  "server1",
			},
		}

		metrics, err := transformer.Transform(results)
		require.NoError(t, err)
		require.Len(t, metrics, 1)

		metric := metrics[0]
		require.Equal(t, "team_legacy_metric", metric.Name)
		require.Equal(t, 42.0, metric.Value)
		require.Equal(t, map[string]string{"host": "server1"}, metric.Labels)
	})

	t.Run("prefer ValueColumns over ValueColumn when both are set", func(t *testing.T) {
		config := TransformConfig{
			DefaultMetricName: "test_metric",
			ValueColumn:       "old_value",                          // Should be ignored
			ValueColumns:      []string{"new_value1", "new_value2"}, // Should be used
			LabelColumns:      []string{"host"},
		}
		meter := noop.NewMeterProvider().Meter("test")
		transformer := NewKustoToMetricsTransformer(config, meter)

		results := []map[string]any{
			{
				"old_value":  100.0, // Should be ignored
				"new_value1": 200.0, // Should be used
				"new_value2": 300.0, // Should be used
				"host":       "server1",
			},
		}

		metrics, err := transformer.Transform(results)
		require.NoError(t, err)
		require.Len(t, metrics, 2) // Should generate 2 metrics from ValueColumns

		// Sort metrics by name for consistent assertion
		if metrics[0].Name > metrics[1].Name {
			metrics[0], metrics[1] = metrics[1], metrics[0]
		}

		require.Equal(t, "test_metric_new_value1", metrics[0].Name)
		require.Equal(t, 200.0, metrics[0].Value)

		require.Equal(t, "test_metric_new_value2", metrics[1].Name)
		require.Equal(t, 300.0, metrics[1].Value)
	})
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

func TestNormalizeColumnName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic cases
		{
			name:     "simple lowercase",
			input:    "metric",
			expected: "metric",
		},
		{
			name:     "camelCase to snake_case",
			input:    "SuccessfulRequests",
			expected: "successful_requests",
		},
		{
			name:     "mixed case with numbers",
			input:    "AvgLatency99",
			expected: "avg_latency99",
		},

		// Special character handling
		{
			name:     "hyphen replacement",
			input:    "Total-Count",
			expected: "total_count",
		},
		{
			name:     "dot replacement",
			input:    "response.time",
			expected: "response_time",
		},
		{
			name:     "space replacement",
			input:    "Success Rate",
			expected: "success_rate",
		},
		{
			name:     "multiple special chars",
			input:    "Server@Health#Status!",
			expected: "server_health_status",
		},

		// Consecutive underscore handling
		{
			name:     "multiple consecutive underscores",
			input:    "metric__with___many____underscores",
			expected: "metric_with_many_underscores",
		},
		{
			name:     "leading and trailing underscores",
			input:    "_metric_name_",
			expected: "metric_name",
		},
		{
			name:     "mixed special chars creating underscores",
			input:    "metric--with..multiple@@chars",
			expected: "metric_with_multiple_chars",
		},

		// Number handling
		{
			name:     "starting with number",
			input:    "95thPercentile",
			expected: "_95th_percentile",
		},
		{
			name:     "only numbers",
			input:    "123",
			expected: "_123",
		},
		{
			name:     "numbers in middle",
			input:    "P99Latency",
			expected: "p99_latency",
		},

		// Edge cases
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@#$%",
			expected: "metric",
		},
		{
			name:     "only underscores",
			input:    "____",
			expected: "metric",
		},
		{
			name:     "single character",
			input:    "A",
			expected: "a",
		},
		{
			name:     "single number",
			input:    "5",
			expected: "_5",
		},

		// Real-world examples
		{
			name:     "azure metric style",
			input:    "CPUUtilizationPercent",
			expected: "cpu_utilization_percent",
		},
		{
			name:     "kubernetes style",
			input:    "container_memory_usage_bytes",
			expected: "container_memory_usage_bytes",
		},
		{
			name:     "prometheus style already normalized",
			input:    "http_requests_total",
			expected: "http_requests_total",
		},
		{
			name:     "database column style",
			input:    "TotalRequestCount",
			expected: "total_request_count",
		},
		{
			name:     "mixed separators",
			input:    "API-Response.Time_ms",
			expected: "api_response_time_ms",
		},

		// Unicode and international characters
		{
			name:     "unicode characters",
			input:    "métrïc_nåme",
			expected: "m_tr_c_n_me",
		},
		{
			name:     "accented characters",
			input:    "latência_média",
			expected: "lat_ncia_m_dia",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeColumnName(tt.input)
			require.Equal(t, tt.expected, result, "normalizeColumnName(%q) = %q, want %q", tt.input, result, tt.expected)

			// Verify the result follows Prometheus naming conventions
			if tt.expected != "" && tt.expected != "metric" {
				// Should be lowercase
				require.Equal(t, strings.ToLower(result), result, "result should be lowercase")

				// Should start with letter or underscore
				if len(result) > 0 {
					firstChar := result[0]
					require.True(t,
						(firstChar >= 'a' && firstChar <= 'z') || firstChar == '_',
						"result should start with letter or underscore, got %c", firstChar)
				}

				// Should contain only valid characters
				for i, char := range result {
					isValid := (char >= 'a' && char <= 'z') ||
						(char >= '0' && char <= '9') ||
						char == '_'
					require.True(t, isValid, "invalid character %c at position %d in result %q", char, i, result)
				}

				// Should not have consecutive underscores
				require.NotContains(t, result, "__", "result should not contain consecutive underscores")
			}
		})
	}
}

func TestNormalizeColumnNamePerformance(t *testing.T) {
	// Test with a reasonably complex input
	input := "Very-Complex@Metric#Name$With%Many^Special&Characters*And(Numbers)123[Brackets]"

	// Run multiple iterations to ensure consistent behavior
	var results []string
	for i := 0; i < 100; i++ {
		result := normalizeColumnName(input)
		results = append(results, result)
	}

	// Verify all results are identical (deterministic)
	expected := results[0]
	for i, result := range results {
		require.Equal(t, expected, result, "result at iteration %d differs from first result", i)
	}

	// Verify the expected transformation
	require.Equal(t, "very_complex_metric_name_with_many_special_characters_and_numbers_123_brackets", expected)
}

func BenchmarkNormalizeColumnName(b *testing.B) {
	testCases := []struct {
		name  string
		input string
	}{
		{"simple", "SimpleMetric"},
		{"complex", "Very-Complex@Metric#Name$With%Many^Special&Characters*123"},
		{"long", "ThisIsAVeryLongMetricNameWithManyWordsAndSpecialCharacters@#$%^&*()1234567890"},
		{"already_normalized", "already_normalized_metric_name"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = normalizeColumnName(tc.input)
			}
		})
	}
}

func TestConstructMetricName(t *testing.T) {
	tests := []struct {
		name       string
		baseName   string
		prefix     string
		columnName string
		expected   string
	}{
		// Basic cases
		{
			name:       "full metric with all components",
			baseName:   "customer_success_rate",
			prefix:     "teama",
			columnName: "Numerator",
			expected:   "teama_customer_success_rate_numerator",
		},
		{
			name:       "no prefix provided",
			baseName:   "response_time",
			prefix:     "",
			columnName: "AvgLatency",
			expected:   "response_time_avg_latency",
		},
		{
			name:       "no base name provided",
			baseName:   "",
			prefix:     "teamb",
			columnName: "Count",
			expected:   "teamb_count",
		},
		{
			name:       "only column name provided",
			baseName:   "",
			prefix:     "",
			columnName: "SuccessfulRequests",
			expected:   "successful_requests",
		},

		// Real-world examples from design document
		{
			name:       "design doc example 1",
			baseName:   "customer_success_rate",
			prefix:     "teama",
			columnName: "Numerator",
			expected:   "teama_customer_success_rate_numerator",
		},
		{
			name:       "design doc example 2",
			baseName:   "customer_success_rate",
			prefix:     "teama",
			columnName: "Denominator",
			expected:   "teama_customer_success_rate_denominator",
		},
		{
			name:       "no prefix example",
			baseName:   "api_response",
			prefix:     "",
			columnName: "AvgLatency",
			expected:   "api_response_avg_latency",
		},

		// CamelCase handling
		{
			name:       "camelCase base name",
			baseName:   "CustomerSuccessRate",
			prefix:     "teamc",
			columnName: "Numerator",
			expected:   "teamc_customer_success_rate_numerator",
		},
		{
			name:       "camelCase prefix",
			baseName:   "api_metrics",
			prefix:     "TeamABC",
			columnName: "Count",
			expected:   "team_abc_api_metrics_count",
		},
		{
			name:       "complex camelCase column",
			baseName:   "service_health",
			prefix:     "prod",
			columnName: "CPUUtilizationPercent",
			expected:   "prod_service_health_cpu_utilization_percent",
		},

		// Special character handling
		{
			name:       "special chars in base name",
			baseName:   "API-Response.Time",
			prefix:     "team-a",
			columnName: "P99_Latency",
			expected:   "team_a_api_response_time_p99_latency",
		},
		{
			name:       "mixed separators",
			baseName:   "http.requests@total",
			prefix:     "monitoring_team",
			columnName: "Success-Rate",
			expected:   "monitoring_team_http_requests_total_success_rate",
		},

		// Edge cases
		{
			name:       "empty column name",
			baseName:   "test_metric",
			prefix:     "team",
			columnName: "",
			expected:   "team_test_metric_value",
		},
		{
			name:       "all empty strings",
			baseName:   "",
			prefix:     "",
			columnName: "",
			expected:   "value",
		},
		{
			name:       "special chars only in column",
			baseName:   "valid_metric",
			prefix:     "team",
			columnName: "@#$%",
			expected:   "team_valid_metric_value",
		},
		{
			name:       "numbers in column name",
			baseName:   "latency",
			prefix:     "",
			columnName: "95thPercentile",
			expected:   "latency_95th_percentile",
		},
		{
			name:       "column starting with number",
			baseName:   "response",
			prefix:     "api",
			columnName: "99Percentile",
			expected:   "api_response_99_percentile",
		},

		// Unicode and international characters
		{
			name:       "unicode in column name",
			baseName:   "metrics",
			prefix:     "team",
			columnName: "latência_média",
			expected:   "team_metrics_lat_ncia_m_dia",
		},

		// Long names
		{
			name:       "very long metric name",
			baseName:   "very_long_service_name_with_many_components",
			prefix:     "extremely_long_team_name_prefix",
			columnName: "VeryLongColumnNameWithManyWords",
			expected:   "extremely_long_team_name_prefix_very_long_service_name_with_many_components_very_long_column_name_with_many_words",
		},

		// Already normalized components
		{
			name:       "already normalized components",
			baseName:   "http_requests_total",
			prefix:     "monitoring_team",
			columnName: "success_rate",
			expected:   "monitoring_team_http_requests_total_success_rate",
		},

		// Single character components
		{
			name:       "single character components",
			baseName:   "x",
			prefix:     "a",
			columnName: "y",
			expected:   "a_x_y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructMetricName(tt.baseName, tt.prefix, tt.columnName)
			require.Equal(t, tt.expected, result,
				"constructMetricName(%q, %q, %q) = %q, want %q",
				tt.baseName, tt.prefix, tt.columnName, result, tt.expected)

			// Verify the result follows Prometheus naming conventions
			if tt.expected != "metric_value" && tt.expected != "value" {
				// Should be lowercase
				require.Equal(t, strings.ToLower(result), result, "result should be lowercase")

				// Should start with letter or underscore
				if len(result) > 0 {
					firstChar := result[0]
					require.True(t,
						(firstChar >= 'a' && firstChar <= 'z') || firstChar == '_',
						"result should start with letter or underscore, got %c", firstChar)
				}

				// Should contain only valid characters
				for i, char := range result {
					isValid := (char >= 'a' && char <= 'z') ||
						(char >= '0' && char <= '9') ||
						char == '_'
					require.True(t, isValid, "invalid character %c at position %d in result %q", char, i, result)
				}

				// Should not have consecutive underscores
				require.NotContains(t, result, "__", "result should not contain consecutive underscores")

				// Should not start or end with underscore (unless it's a number-prefixed metric)
				if len(result) > 0 && result[0] != '_' {
					require.False(t, strings.HasSuffix(result, "_"), "result should not end with underscore")
				}
			}
		})
	}
}

func TestConstructMetricNameEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		baseName    string
		prefix      string
		columnName  string
		description string
	}{
		{
			name:        "normalization fallback to value",
			baseName:    "test",
			prefix:      "",
			columnName:  "____", // Will normalize to empty, should fallback to "value"
			description: "column that normalizes to empty should use 'value' fallback",
		},
		{
			name:        "prefix normalization fallback",
			baseName:    "test",
			prefix:      "@@@", // Will normalize to "metric", should be ignored
			columnName:  "count",
			description: "prefix that normalizes to 'metric' should be ignored",
		},
		{
			name:        "base name normalization fallback",
			baseName:    "###", // Will normalize to "metric", should be ignored
			prefix:      "team",
			columnName:  "value",
			description: "base name that normalizes to 'metric' should be ignored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := constructMetricName(tt.baseName, tt.prefix, tt.columnName)

			// Result should always be a valid metric name
			require.NotEmpty(t, result, "result should never be empty")
			require.True(t, len(result) > 0, "result should have content")

			// Should follow Prometheus conventions
			require.Equal(t, strings.ToLower(result), result, "result should be lowercase")
			if len(result) > 0 {
				firstChar := result[0]
				require.True(t,
					(firstChar >= 'a' && firstChar <= 'z') || firstChar == '_',
					"result should start with letter or underscore")
			}

			t.Logf("Test case: %s", tt.description)
			t.Logf("Result: %s", result)
		})
	}
}

func TestConstructMetricNamePerformance(t *testing.T) {
	// Test with various complexity scenarios
	scenarios := []struct {
		baseName   string
		prefix     string
		columnName string
	}{
		{"simple", "", "count"},
		{"complex_service_name", "team_prefix", "ComplexColumnName"},
		{"very_long_service_name_with_many_components", "extremely_long_team_prefix", "VeryLongColumnNameWithManyWordsAndNumbers123"},
	}

	// Run multiple iterations to ensure consistent behavior
	for _, scenario := range scenarios {
		var results []string
		for i := 0; i < 100; i++ {
			result := constructMetricName(scenario.baseName, scenario.prefix, scenario.columnName)
			results = append(results, result)
		}

		// Verify all results are identical (deterministic)
		expected := results[0]
		for i, result := range results {
			require.Equal(t, expected, result, "result at iteration %d differs from first result", i)
		}
	}
}

func BenchmarkConstructMetricName(b *testing.B) {
	testCases := []struct {
		name       string
		baseName   string
		prefix     string
		columnName string
	}{
		{"simple", "metric", "", "count"},
		{"with_prefix", "service_metric", "team", "value"},
		{"complex", "complex_service_name", "team_prefix", "ComplexColumnName"},
		{"long", "very_long_service_name_with_many_components", "extremely_long_team_prefix", "VeryLongColumnNameWithManyWords"},
		{"camelcase_heavy", "CustomerServiceMetrics", "TeamABCDEF", "CPUUtilizationPercentileP99"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = constructMetricName(tc.baseName, tc.prefix, tc.columnName)
			}
		})
	}
}

func TestTransformConfigMultiValue(t *testing.T) {
	tests := []struct {
		name        string
		config      TransformConfig
		description string
	}{
		{
			name: "multi-value config with prefix",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				MetricNamePrefix: "teama",
				ValueColumns:     []string{"Numerator", "Denominator", "AvgLatency"},
				TimestampColumn:  "timestamp",
				LabelColumns:     []string{"LocationId", "ServiceTier"},
			},
			description: "full multi-value configuration with team prefix",
		},
		{
			name: "multi-value config without prefix",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				ValueColumns:     []string{"Count", "ErrorRate"},
				TimestampColumn:  "timestamp",
				LabelColumns:     []string{"Region"},
			},
			description: "multi-value configuration without prefix",
		},
		{
			name: "legacy single-value config",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				ValueColumn:      "value",
				TimestampColumn:  "timestamp",
				LabelColumns:     []string{"service"},
			},
			description: "legacy single-value configuration for backward compatibility",
		},
		{
			name: "mixed config (should prefer ValueColumns)",
			config: TransformConfig{
				MetricNameColumn: "metric_name",
				ValueColumn:      "old_value",
				ValueColumns:     []string{"new_value"},
				TimestampColumn:  "timestamp",
			},
			description: "config with both ValueColumn and ValueColumns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter := noop.NewMeterProvider().Meter("test")
			transformer := NewKustoToMetricsTransformer(tt.config, meter)

			require.Equal(t, tt.config.MetricNameColumn, transformer.config.MetricNameColumn)
			require.Equal(t, tt.config.MetricNamePrefix, transformer.config.MetricNamePrefix)
			require.Equal(t, tt.config.ValueColumn, transformer.config.ValueColumn)
			require.Equal(t, tt.config.ValueColumns, transformer.config.ValueColumns)
			require.Equal(t, tt.config.TimestampColumn, transformer.config.TimestampColumn)
			require.Equal(t, tt.config.LabelColumns, transformer.config.LabelColumns)

			t.Logf("Test case: %s", tt.description)
		})
	}
}

func TestExtractValues(t *testing.T) {
	config := TransformConfig{
		ValueColumns: []string{"Numerator", "Denominator", "AvgLatency"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	tests := []struct {
		name         string
		row          map[string]any
		valueColumns []string
		expected     map[string]float64
		wantErr      bool
		errMsg       string
	}{
		{
			name: "successful extraction of multiple values",
			row: map[string]any{
				"Numerator":   1974.0,
				"Denominator": 2000.0,
				"AvgLatency":  150.2,
				"LocationId":  "datacenter-01",
			},
			valueColumns: []string{"Numerator", "Denominator", "AvgLatency"},
			expected: map[string]float64{
				"Numerator":   1974.0,
				"Denominator": 2000.0,
				"AvgLatency":  150.2,
			},
			wantErr: false,
		},
		{
			name: "extraction with different numeric types",
			row: map[string]any{
				"IntValue":   int(42),
				"Int32Value": int32(123),
				"Int64Value": int64(456),
				"Float32Val": float32(3.14),
				"Float64Val": float64(2.718),
			},
			valueColumns: []string{"IntValue", "Int32Value", "Int64Value", "Float32Val", "Float64Val"},
			expected: map[string]float64{
				"IntValue":   42.0,
				"Int32Value": 123.0,
				"Int64Value": 456.0,
				"Float32Val": 3.140000104904175, // float32 precision
				"Float64Val": 2.718,
			},
			wantErr: false,
		},
		{
			name: "extraction with Kusto value types",
			row: map[string]any{
				"LongValue": value.Long{Value: 12345, Valid: true},
				"RealValue": value.Real{Value: 98.76, Valid: true},
				"IntValue":  value.Int{Value: 999, Valid: true},
			},
			valueColumns: []string{"LongValue", "RealValue", "IntValue"},
			expected: map[string]float64{
				"LongValue": 12345.0,
				"RealValue": 98.76,
				"IntValue":  999.0,
			},
			wantErr: false,
		},
		{
			name: "extraction with string numeric values",
			row: map[string]any{
				"StringInt":   "42",
				"StringFloat": "3.14159",
				"RegularInt":  100,
			},
			valueColumns: []string{"StringInt", "StringFloat", "RegularInt"},
			expected: map[string]float64{
				"StringInt":   42.0,
				"StringFloat": 3.14159,
				"RegularInt":  100.0,
			},
			wantErr: false,
		},
		{
			name: "missing column error",
			row: map[string]any{
				"ExistingColumn": 42.0,
			},
			valueColumns: []string{"ExistingColumn", "MissingColumn"},
			expected:     nil,
			wantErr:      true,
			errMsg:       "value column 'MissingColumn' not found in row",
		},
		{
			name: "null value error",
			row: map[string]any{
				"ValidColumn": 42.0,
				"NullColumn":  nil,
			},
			valueColumns: []string{"ValidColumn", "NullColumn"},
			expected:     nil,
			wantErr:      true,
			errMsg:       "value column 'NullColumn' contains null value",
		},
		{
			name: "invalid Kusto value",
			row: map[string]any{
				"ValidColumn":  42.0,
				"InvalidValue": value.Long{Value: 0, Valid: false},
			},
			valueColumns: []string{"ValidColumn", "InvalidValue"},
			expected:     nil,
			wantErr:      true,
			errMsg:       "value column 'InvalidValue' contains null value",
		},
		{
			name: "unparseable string value",
			row: map[string]any{
				"ValidColumn":      42.0,
				"InvalidStringVal": "not-a-number",
			},
			valueColumns: []string{"ValidColumn", "InvalidStringVal"},
			expected:     nil,
			wantErr:      true,
			errMsg:       "contains unparseable string value",
		},
		{
			name: "unsupported type",
			row: map[string]any{
				"ValidColumn": 42.0,
				"InvalidType": []string{"array", "not", "supported"},
			},
			valueColumns: []string{"ValidColumn", "InvalidType"},
			expected:     nil,
			wantErr:      true,
			errMsg:       "contains unsupported type",
		},
		{
			name: "empty value columns list",
			row: map[string]any{
				"SomeColumn": 42.0,
			},
			valueColumns: []string{},
			expected:     map[string]float64{},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.extractValues(tt.row, tt.valueColumns)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, len(tt.expected), len(result))

				for key, expectedValue := range tt.expected {
					actualValue, exists := result[key]
					require.True(t, exists, "missing key %s in result", key)
					require.InDelta(t, expectedValue, actualValue, 0.000001,
						"value mismatch for key %s: expected %f, got %f", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestExtractValueFromColumn(t *testing.T) {
	config := TransformConfig{ValueColumns: []string{"test"}}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	tests := []struct {
		name       string
		row        map[string]any
		columnName string
		expected   float64
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "extract float64",
			row:        map[string]any{"test_col": 42.5},
			columnName: "test_col",
			expected:   42.5,
			wantErr:    false,
		},
		{
			name:       "extract int",
			row:        map[string]any{"test_col": 100},
			columnName: "test_col",
			expected:   100.0,
			wantErr:    false,
		},
		{
			name:       "extract Kusto Long",
			row:        map[string]any{"test_col": value.Long{Value: 12345, Valid: true}},
			columnName: "test_col",
			expected:   12345.0,
			wantErr:    false,
		},
		{
			name:       "extract string number",
			row:        map[string]any{"test_col": "3.14159"},
			columnName: "test_col",
			expected:   3.14159,
			wantErr:    false,
		},
		{
			name:       "missing column",
			row:        map[string]any{"other_col": 42.0},
			columnName: "missing_col",
			expected:   0,
			wantErr:    true,
			errMsg:     "value column 'missing_col' not found in row",
		},
		{
			name:       "null value",
			row:        map[string]any{"test_col": nil},
			columnName: "test_col",
			expected:   0,
			wantErr:    true,
			errMsg:     "value column 'test_col' contains null value",
		},
		{
			name:       "invalid Kusto Real",
			row:        map[string]any{"test_col": value.Real{Value: 0, Valid: false}},
			columnName: "test_col",
			expected:   0,
			wantErr:    true,
			errMsg:     "value column 'test_col' contains null value",
		},
		{
			name:       "unparseable string",
			row:        map[string]any{"test_col": "not-a-number"},
			columnName: "test_col",
			expected:   0,
			wantErr:    true,
			errMsg:     "contains unparseable string value",
		},
		{
			name:       "unsupported type",
			row:        map[string]any{"test_col": map[string]string{"key": "value"}},
			columnName: "test_col",
			expected:   0,
			wantErr:    true,
			errMsg:     "contains unsupported type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := transformer.extractValueFromColumn(tt.row, tt.columnName)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.InDelta(t, tt.expected, result, 0.000001)
			}
		})
	}
}

func TestExtractValuesPerformance(t *testing.T) {
	config := TransformConfig{
		ValueColumns: []string{"Col1", "Col2", "Col3", "Col4", "Col5"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	// Create a test row with multiple columns
	row := map[string]any{
		"Col1":  1.0,
		"Col2":  2.0,
		"Col3":  3.0,
		"Col4":  4.0,
		"Col5":  5.0,
		"Col6":  6.0,
		"Col7":  7.0,
		"Col8":  8.0,
		"Col9":  9.0,
		"Col10": 10.0,
	}

	// Test different numbers of columns
	testCases := []struct {
		name    string
		columns []string
	}{
		{"single_column", []string{"Col1"}},
		{"three_columns", []string{"Col1", "Col2", "Col3"}},
		{"five_columns", []string{"Col1", "Col2", "Col3", "Col4", "Col5"}},
		{"ten_columns", []string{"Col1", "Col2", "Col3", "Col4", "Col5", "Col6", "Col7", "Col8", "Col9", "Col10"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Run multiple iterations to ensure consistent behavior
			var results []map[string]float64
			for i := 0; i < 100; i++ {
				result, err := transformer.extractValues(row, tc.columns)
				require.NoError(t, err)
				results = append(results, result)
			}

			// Verify all results are identical (deterministic)
			expected := results[0]
			for i, result := range results {
				require.Equal(t, expected, result, "result at iteration %d differs from first result", i)
			}

			// Verify correct number of values extracted
			require.Equal(t, len(tc.columns), len(expected))
		})
	}
}

func BenchmarkExtractValues(b *testing.B) {
	config := TransformConfig{ValueColumns: []string{"test"}}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToMetricsTransformer(config, meter)

	testCases := []struct {
		name    string
		row     map[string]any
		columns []string
	}{
		{
			"single_column",
			map[string]any{"col1": 42.0},
			[]string{"col1"},
		},
		{
			"three_columns",
			map[string]any{"col1": 42.0, "col2": 43.0, "col3": 44.0},
			[]string{"col1", "col2", "col3"},
		},
		{
			"five_columns_mixed_types",
			map[string]any{
				"col1": 42.0,
				"col2": int(43),
				"col3": value.Long{Value: 44, Valid: true},
				"col4": "45.5",
				"col5": float32(46.5),
			},
			[]string{"col1", "col2", "col3", "col4", "col5"},
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = transformer.extractValues(tc.row, tc.columns)
			}
		})
	}
}
