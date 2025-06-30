package transform

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestNewKustoToPrometheusTransformer(t *testing.T) {
	config := TransformConfig{
		ValueColumn: "value",
	}
	meter := noop.NewMeterProvider().Meter("test")

	transformer := NewKustoToPrometheusTransformer(config, meter)

	if transformer.config.ValueColumn != "value" {
		t.Errorf("Expected ValueColumn to be 'value', got '%s'", transformer.config.ValueColumn)
	}
	if transformer.meter == nil {
		t.Error("Expected meter to be set")
	}
}

func TestTransformBasic(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

	results := []map[string]any{
		{
			"value": 42.0,
		},
	}

	metrics, err := transformer.Transform(results)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	if metric.Name != "test_metric" {
		t.Errorf("Expected metric name 'test_metric', got '%s'", metric.Name)
	}
	if metric.Value != 42.0 {
		t.Errorf("Expected metric value 42.0, got %f", metric.Value)
	}
}

func TestTransformWithMetricNameColumn(t *testing.T) {
	config := TransformConfig{
		MetricNameColumn: "metric_name",
		ValueColumn:      "value",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

	results := []map[string]any{
		{
			"metric_name": "cpu_usage",
			"value":       85.5,
		},
	}

	metrics, err := transformer.Transform(results)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	if metric.Name != "cpu_usage" {
		t.Errorf("Expected metric name 'cpu_usage', got '%s'", metric.Name)
	}
	if metric.Value != 85.5 {
		t.Errorf("Expected metric value 85.5, got %f", metric.Value)
	}
}

func TestTransformWithLabels(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
		LabelColumns:      []string{"host", "service"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

	results := []map[string]any{
		{
			"value":   100.0,
			"host":    "server1",
			"service": "api",
		},
	}

	metrics, err := transformer.Transform(results)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	if len(metric.Labels) != 2 {
		t.Fatalf("Expected 2 labels, got %d", len(metric.Labels))
	}
	if metric.Labels["host"] != "server1" {
		t.Errorf("Expected host label 'server1', got '%s'", metric.Labels["host"])
	}
	if metric.Labels["service"] != "api" {
		t.Errorf("Expected service label 'api', got '%s'", metric.Labels["service"])
	}
}

func TestTransformWithTimestamp(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
		TimestampColumn:   "timestamp",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

	testTime := time.Date(2023, 12, 25, 12, 0, 0, 0, time.UTC)
	results := []map[string]any{
		{
			"value":     50.0,
			"timestamp": testTime,
		},
	}

	metrics, err := transformer.Transform(results)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(metrics) != 1 {
		t.Fatalf("Expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	if !metric.Timestamp.Equal(testTime) {
		t.Errorf("Expected timestamp %v, got %v", testTime, metric.Timestamp)
	}
}

func TestTransformValueTypes(t *testing.T) {
	config := TransformConfig{
		ValueColumn:       "value",
		DefaultMetricName: "test_metric",
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

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
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if len(metrics) != 1 {
				t.Fatalf("Expected 1 metric, got %d", len(metrics))
			}

			if metrics[0].Value != tc.expected {
				t.Errorf("Expected value %f, got %f", tc.expected, metrics[0].Value)
			}
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
	transformer := NewKustoToPrometheusTransformer(config, meter)

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
			if err != nil {
				t.Fatalf("Transform failed: %v", err)
			}

			if len(metrics) != 1 {
				t.Fatalf("Expected 1 metric, got %d", len(metrics))
			}

			if !metrics[0].Timestamp.Equal(tc.expected) {
				t.Errorf("Expected timestamp %v, got %v", tc.expected, metrics[0].Timestamp)
			}
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
			transformer := NewKustoToPrometheusTransformer(tc.config, meter)
			_, err := transformer.Transform(tc.results)
			if tc.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
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
			transformer := NewKustoToPrometheusTransformer(tc.config, meter)
			err := transformer.Validate(tc.results)
			if tc.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestRegisterMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(TransformConfig{}, meter)

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
	if err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}
}

func TestTransformMultipleRows(t *testing.T) {
	config := TransformConfig{
		MetricNameColumn: "metric",
		ValueColumn:      "value",
		LabelColumns:     []string{"host"},
	}
	meter := noop.NewMeterProvider().Meter("test")
	transformer := NewKustoToPrometheusTransformer(config, meter)

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
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	if len(metrics) != 3 {
		t.Fatalf("Expected 3 metrics, got %d", len(metrics))
	}

	// Verify first metric
	if metrics[0].Name != "cpu_usage" || metrics[0].Value != 85.5 || metrics[0].Labels["host"] != "server1" {
		t.Errorf("First metric incorrect: %+v", metrics[0])
	}

	// Verify second metric
	if metrics[1].Name != "memory_usage" || metrics[1].Value != 60.2 || metrics[1].Labels["host"] != "server1" {
		t.Errorf("Second metric incorrect: %+v", metrics[1])
	}

	// Verify third metric
	if metrics[2].Name != "cpu_usage" || metrics[2].Value != 92.1 || metrics[2].Labels["host"] != "server2" {
		t.Errorf("Third metric incorrect: %+v", metrics[2])
	}
}
