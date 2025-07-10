package transform

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TransformConfig defines how to convert KQL query results to metrics format
type TransformConfig struct {
	// MetricNameColumn specifies which column contains the metric name
	MetricNameColumn string `json:"metricNameColumn,omitempty"`

	// ValueColumn specifies which column contains the metric value
	ValueColumn string `json:"valueColumn"`

	// TimestampColumn specifies which column contains the timestamp
	TimestampColumn string `json:"timestampColumn"`

	// LabelColumns specifies columns to use as metric labels
	LabelColumns []string `json:"labelColumns,omitempty"`

	// DefaultMetricName provides a fallback if MetricNameColumn is not specified
	DefaultMetricName string `json:"defaultMetricName,omitempty"`
}

// KustoToMetricsTransformer transforms KQL query results to Prometheus metrics
type KustoToMetricsTransformer struct {
	config TransformConfig
	meter  metric.Meter
}

// MetricData represents a single metric data point
type MetricData struct {
	Name      string
	Value     float64
	Timestamp time.Time
	Labels    map[string]string
}

// NewKustoToMetricsTransformer creates a new transformer instance
func NewKustoToMetricsTransformer(config TransformConfig, meter metric.Meter) *KustoToMetricsTransformer {
	return &KustoToMetricsTransformer{
		config: config,
		meter:  meter,
	}
}

// Transform converts KQL query results to Prometheus metrics
func (t *KustoToMetricsTransformer) Transform(results []map[string]any) ([]MetricData, error) {
	if len(results) == 0 {
		return []MetricData{}, nil
	}

	var metrics []MetricData

	for i, row := range results {
		metric, err := t.transformRow(row)
		if err != nil {
			return nil, fmt.Errorf("failed to transform row %d: %w", i, err)
		}
		metrics = append(metrics, metric)
	}

	return metrics, nil
}

// transformRow converts a single KQL result row to metric data
func (t *KustoToMetricsTransformer) transformRow(row map[string]any) (MetricData, error) {
	// Extract metric name
	metricName, err := t.extractMetricName(row)
	if err != nil {
		return MetricData{}, fmt.Errorf("failed to extract metric name: %w", err)
	}

	// Extract metric value
	value, err := t.extractValue(row)
	if err != nil {
		return MetricData{}, fmt.Errorf("failed to extract value: %w", err)
	}

	// Extract timestamp
	timestamp, err := t.extractTimestamp(row)
	if err != nil {
		return MetricData{}, fmt.Errorf("failed to extract timestamp: %w", err)
	}

	// Extract labels
	labels, err := t.extractLabels(row)
	if err != nil {
		return MetricData{}, fmt.Errorf("failed to extract labels: %w", err)
	}

	return MetricData{
		Name:      metricName,
		Value:     value,
		Timestamp: timestamp,
		Labels:    labels,
	}, nil
}

// extractMetricName extracts the metric name from the row
func (t *KustoToMetricsTransformer) extractMetricName(row map[string]any) (string, error) {
	// If metric name column is specified, use it
	if t.config.MetricNameColumn != "" {
		rawValue, exists := row[t.config.MetricNameColumn]
		if !exists {
			return "", fmt.Errorf("metric name column '%s' not found in row", t.config.MetricNameColumn)
		}

		switch v := rawValue.(type) {
		case string:
			if v == "" {
				return "", fmt.Errorf("metric name column '%s' contains empty string", t.config.MetricNameColumn)
			}
			return v, nil
		case value.String:
			if !v.Valid {
				return "", fmt.Errorf("metric name column '%s' contains null value", t.config.MetricNameColumn)
			}
			if v.Value == "" {
				return "", fmt.Errorf("metric name column '%s' contains empty string", t.config.MetricNameColumn)
			}
			return v.Value, nil
		default:
			return "", fmt.Errorf("metric name column '%s' contains non-string value: %T", t.config.MetricNameColumn, rawValue)
		}
	}

	// If default metric name is provided, use it
	if t.config.DefaultMetricName != "" {
		return t.config.DefaultMetricName, nil
	}

	// No metric name configuration
	return "", fmt.Errorf("no metric name configuration: neither metricNameColumn nor defaultMetricName specified")
}

// extractValue extracts the numeric value from the row
func (t *KustoToMetricsTransformer) extractValue(row map[string]any) (float64, error) {
	rawValue, exists := row[t.config.ValueColumn]
	if !exists {
		return 0, fmt.Errorf("value column '%s' not found in row", t.config.ValueColumn)
	}

	if rawValue == nil {
		return 0, fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
	}

	// Handle different numeric types from KQL
	switch v := rawValue.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case value.Long:
		if !v.Valid {
			return 0, fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
		return float64(v.Value), nil
	case value.Real:
		if !v.Valid {
			return 0, fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
		return v.Value, nil
	case value.Int:
		if !v.Valid {
			return 0, fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
		return float64(v.Value), nil
	case string:
		// Try to parse string as float
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("value column '%s' contains unparseable string value '%s': %w", t.config.ValueColumn, v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("value column '%s' contains unsupported type %T", t.config.ValueColumn, rawValue)
	}
}

// extractTimestamp extracts the timestamp from the row
func (t *KustoToMetricsTransformer) extractTimestamp(row map[string]any) (time.Time, error) {
	// If no timestamp column is configured, use current time
	if t.config.TimestampColumn == "" {
		return time.Now(), nil
	}

	rawValue, exists := row[t.config.TimestampColumn]
	if !exists {
		return time.Time{}, fmt.Errorf("timestamp column '%s' not found in row", t.config.TimestampColumn)
	}

	if rawValue == nil {
		return time.Time{}, fmt.Errorf("timestamp column '%s' contains null value", t.config.TimestampColumn)
	}

	// Handle different timestamp types from KQL
	switch v := rawValue.(type) {
	case time.Time:
		return v, nil
	case string:
		// Try to parse string as RFC3339 timestamp
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			// Try alternative formats
			for _, format := range []string{
				time.RFC3339Nano,
				"2006-01-02T15:04:05Z",
				"2006-01-02T15:04:05.999Z",
				"2006-01-02 15:04:05",
			} {
				if parsed, err = time.Parse(format, v); err == nil {
					return parsed, nil
				}
			}
			return time.Time{}, fmt.Errorf("timestamp column '%s' contains unparseable string value '%s': %w", t.config.TimestampColumn, v, err)
		}
		return parsed, nil
	default:
		return time.Time{}, fmt.Errorf("timestamp column '%s' contains unsupported type %T", t.config.TimestampColumn, rawValue)
	}
}

// extractLabels extracts label key-value pairs from the row
func (t *KustoToMetricsTransformer) extractLabels(row map[string]any) (map[string]string, error) {
	labels := make(map[string]string)

	for _, labelColumn := range t.config.LabelColumns {
		rawValue, exists := row[labelColumn]
		if !exists {
			// Skip missing label columns instead of failing
			continue
		}

		// Convert value to string
		var labelValue string
		if rawValue == nil {
			labelValue = ""
		} else {
			labelValue = fmt.Sprintf("%v", rawValue)
		}

		labels[labelColumn] = labelValue
	}

	return labels, nil
}

// RegisterMetrics registers the metrics with the OpenTelemetry meter
func (t *KustoToMetricsTransformer) RegisterMetrics(ctx context.Context, metrics []MetricData) error {
	// Group metrics by name for efficient registration
	metricsByName := make(map[string][]MetricData)
	for _, metric := range metrics {
		metricsByName[metric.Name] = append(metricsByName[metric.Name], metric)
	}

	// Register each unique metric name as a gauge
	for metricName, metricData := range metricsByName {
		gauge, err := t.meter.Float64Gauge(metricName)
		if err != nil {
			return fmt.Errorf("failed to create gauge for metric '%s': %w", metricName, err)
		}

		// Record all data points for this metric
		for _, data := range metricData {
			// Convert labels to OpenTelemetry attributes
			attrs := make([]attribute.KeyValue, 0, len(data.Labels))
			for key, value := range data.Labels {
				attrs = append(attrs, attribute.String(key, value))
			}

			// Record the metric value
			gauge.Record(ctx, data.Value, metric.WithAttributes(attrs...))
		}
	}

	return nil
}

// Validate checks if the transform configuration is valid for the given query results
func (t *KustoToMetricsTransformer) Validate(results []map[string]any) error {
	if len(results) == 0 {
		return nil // Empty results are valid
	}

	// Check the first row to validate schema
	firstRow := results[0]

	// Validate value column exists and is numeric
	rawValue, exists := firstRow[t.config.ValueColumn]
	if !exists {
		return fmt.Errorf("required value column '%s' not found in query results", t.config.ValueColumn)
	}

	// Check if value is numeric type
	switch v := rawValue.(type) {
	case float64, float32, int, int32, int64, string:
		// Valid numeric types (string will be validated during parsing)
	case value.Long:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
	case value.Real:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
	case value.Int:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
		}
	case nil:
		return fmt.Errorf("value column '%s' contains null value", t.config.ValueColumn)
	default:
		return fmt.Errorf("value column '%s' contains non-numeric type %T", t.config.ValueColumn, rawValue)
	}

	// Validate metric name configuration
	if t.config.MetricNameColumn != "" {
		_, exists := firstRow[t.config.MetricNameColumn]
		if !exists {
			return fmt.Errorf("metric name column '%s' not found in query results", t.config.MetricNameColumn)
		}
	} else if t.config.DefaultMetricName == "" {
		return fmt.Errorf("no metric name configuration: neither metricNameColumn nor defaultMetricName specified")
	}

	// Validate timestamp column if specified
	if t.config.TimestampColumn != "" {
		_, exists := firstRow[t.config.TimestampColumn]
		if !exists {
			return fmt.Errorf("timestamp column '%s' not found in query results", t.config.TimestampColumn)
		}
	}

	// Validate label columns (missing columns are allowed, they'll be skipped)
	availableColumns := make(map[string]bool)
	for column := range firstRow {
		availableColumns[column] = true
	}

	var missingLabelColumns []string
	for _, labelColumn := range t.config.LabelColumns {
		if !availableColumns[labelColumn] {
			missingLabelColumns = append(missingLabelColumns, labelColumn)
		}
	}

	// Log warning about missing label columns but don't fail validation
	if len(missingLabelColumns) > 0 {
		// Note: In a real implementation, this would use a proper logger
		// For now, we'll just document that missing label columns are skipped
	}

	return nil
}
