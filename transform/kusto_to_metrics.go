package transform

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Pre-compiled regular expressions for metric name normalization
var (
	// camelCaseRegex matches lowercase/digit followed by uppercase (camelCase transition)
	camelCaseRegex = regexp.MustCompile(`([a-z0-9])([A-Z])`)

	// acronymRegex matches uppercase followed by uppercase then lowercase (acronym transition)
	acronymRegex = regexp.MustCompile(`([A-Z])([A-Z][a-z])`)

	// nonAlphanumericRegex matches anything that's not a letter, digit, or underscore
	nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9_]`)

	// consecutiveUnderscoresRegex matches one or more consecutive underscores
	consecutiveUnderscoresRegex = regexp.MustCompile(`_+`)
)

// TransformConfig defines how to convert KQL query results to metrics format
type TransformConfig struct {
	// MetricNameColumn specifies which column contains the metric name
	MetricNameColumn string `json:"metricNameColumn,omitempty"`

	// MetricNamePrefix provides optional team/project namespacing for all metrics
	MetricNamePrefix string `json:"metricNamePrefix,omitempty"`

	// ValueColumns specifies columns to use as metric values (required)
	ValueColumns []string `json:"valueColumns"`

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

// normalizeColumnName converts a column name to a valid Prometheus metric name component
// following Prometheus naming conventions:
// - Convert to lowercase
// - Replace non-alphanumeric characters with underscores
// - Remove consecutive underscores
// - Ensure name starts with letter or underscore
// Example: "SuccessfulRequests" → "successful_requests"
func normalizeColumnName(columnName string) (string, error) {
	if columnName == "" {
		return "", errors.New("column name cannot be empty")
	}

	// Convert camelCase to snake_case by inserting underscores before uppercase letters
	// Handle sequences of uppercase letters (like "CPU" -> "CPU") but split at transitions
	// Pattern 1: lowercase/digit followed by uppercase (camelCase transition)
	normalized := camelCaseRegex.ReplaceAllString(columnName, `${1}_${2}`)

	// Pattern 2: uppercase followed by uppercase then lowercase (acronym transition)
	// Example: "CPUUtilization" -> "CPU_Utilization"
	normalized = acronymRegex.ReplaceAllString(normalized, `${1}_${2}`)

	// Convert to lowercase
	normalized = strings.ToLower(normalized)

	// Replace non-alphanumeric characters with underscores
	normalized = nonAlphanumericRegex.ReplaceAllString(normalized, "_")

	// Remove consecutive underscores
	normalized = consecutiveUnderscoresRegex.ReplaceAllString(normalized, "_")

	// Remove leading and trailing underscores
	normalized = strings.Trim(normalized, "_")

	// Ensure name starts with letter or underscore (Prometheus requirement)
	// If it starts with a digit, prepend underscore
	if len(normalized) > 0 && normalized[0] >= '0' && normalized[0] <= '9' {
		normalized = "_" + normalized
	}

	// If somehow we end up with empty string, return a default
	if normalized == "" {
		return "", errors.New("normalized column name cannot be empty")
	}

	return normalized, nil
}

// constructMetricName builds a complete metric name following the pattern: [prefix_]baseName_normalizedColumnName
// Parameters:
//   - baseName: the base metric name (e.g., "customer_success_rate")
//   - prefix: optional team/project prefix (e.g., "teama")
//   - columnName: the value column name to be normalized (e.g., "Numerator")
//
// Examples:
//   - constructMetricName("customer_success_rate", "teama", "Numerator") → "teama_customer_success_rate_numerator"
//   - constructMetricName("response_time", "", "AvgLatency") → "response_time_avg_latency"
//   - constructMetricName("", "teamb", "Count") → "teamb_count"
func constructMetricName(baseName, prefix, columnName string) (string, error) {
	// Normalize the column name to follow Prometheus conventions
	normalizedColumn, err := normalizeColumnName(columnName)
	if err != nil {
		return "", fmt.Errorf("failed to normalize column name '%s': %w", columnName, err)
	}

	var parts []string

	// Add prefix if provided and valid after normalization
	if prefix != "" {
		normalizedPrefix, err := normalizeColumnName(prefix)
		if err != nil {
			return "", fmt.Errorf("failed to normalize prefix '%s': %w", prefix, err)
		}
		parts = append(parts, normalizedPrefix)
	}

	// Add base name if provided and valid after normalization
	if baseName != "" {
		normalizedBase, err := normalizeColumnName(baseName)
		if err != nil {
			return "", fmt.Errorf("failed to normalize base name '%s': %w", baseName, err)
		}
		parts = append(parts, normalizedBase)
	}

	// Always add the normalized column name
	parts = append(parts, normalizedColumn)

	// Join all parts with underscores and clean up
	result := strings.Join(parts, "_")

	// Clean up any double underscores that might have been created
	result = consecutiveUnderscoresRegex.ReplaceAllString(result, "_")

	// Remove leading and trailing underscores
	result = strings.Trim(result, "_")

	// Ensure we have a valid metric name
	if result == "" {
		return "", errors.New("constructed metric name cannot be empty")
	}

	return result, nil
}

// Transform converts KQL query results to Prometheus metrics
// Supports both single-value and multi-value column modes
func (t *KustoToMetricsTransformer) Transform(results []map[string]any) ([]MetricData, error) {
	if len(results) == 0 {
		return []MetricData{}, nil
	}

	var allMetrics []MetricData

	for i, row := range results {
		rowMetrics, err := t.transformRow(row)
		if err != nil {
			return nil, fmt.Errorf("failed to transform row %d: %w", i, err)
		}
		// Append all metrics from this row (could be multiple in multi-value mode)
		allMetrics = append(allMetrics, rowMetrics...)
	}

	return allMetrics, nil
}

// transformRow converts a single KQL result row to metric data
// Generates multiple MetricData objects from ValueColumns
func (t *KustoToMetricsTransformer) transformRow(row map[string]any) ([]MetricData, error) {
	// Extract base metric name
	baseName, err := t.extractMetricName(row)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metric name: %w", err)
	}

	// Extract timestamp (shared across all metrics from this row)
	timestamp, err := t.extractTimestamp(row)
	if err != nil {
		return nil, fmt.Errorf("failed to extract timestamp: %w", err)
	}

	// Extract labels (shared across all metrics from this row)
	labels, err := t.extractLabels(row)
	if err != nil {
		return nil, fmt.Errorf("failed to extract labels: %w", err)
	}

	// Multi-value mode: generate multiple metrics from ValueColumns
	return t.transformRowMultiValue(baseName, row, timestamp, labels)
}

// transformRowMultiValue handles multi-value column transformation
// Generates one metric per value column with constructed names
func (t *KustoToMetricsTransformer) transformRowMultiValue(baseName string, row map[string]any, timestamp time.Time, labels map[string]string) ([]MetricData, error) {
	// Extract values from all configured value columns
	values, err := t.extractValues(row, t.config.ValueColumns)
	if err != nil {
		return nil, fmt.Errorf("failed to extract values: %w", err)
	}

	var metrics []MetricData

	// Generate one metric per value column
	for _, columnName := range t.config.ValueColumns {
		value, exists := values[columnName]
		if !exists {
			return nil, fmt.Errorf("value for column '%s' not found in extracted values", columnName)
		}

		// Construct metric name using prefix, base name, and normalized column name
		metricName, err := constructMetricName(baseName, t.config.MetricNamePrefix, columnName)
		if err != nil {
			return nil, fmt.Errorf("failed to construct metric name for column '%s': %w", columnName, err)
		}

		metrics = append(metrics, MetricData{
			Name:      metricName,
			Value:     value,
			Timestamp: timestamp,
			Labels:    labels, // Shared labels across all metrics from this row
		})
	}

	return metrics, nil
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

// extractValues extracts numeric values from multiple columns in the row
// Returns a map of column name to value for multi-value column transformation
func (t *KustoToMetricsTransformer) extractValues(row map[string]any, valueColumns []string) (map[string]float64, error) {
	values := make(map[string]float64)

	for _, columnName := range valueColumns {
		value, err := t.extractValueFromColumn(row, columnName)
		if err != nil {
			return nil, fmt.Errorf("failed to extract value from column '%s': %w", columnName, err)
		}
		values[columnName] = value
	}

	return values, nil
}

// extractValueFromColumn extracts a numeric value from a specific column
// This is a helper function that reuses the existing numeric type conversion logic
func (t *KustoToMetricsTransformer) extractValueFromColumn(row map[string]any, columnName string) (float64, error) {
	rawValue, exists := row[columnName]
	if !exists {
		return 0, fmt.Errorf("value column '%s' not found in row", columnName)
	}

	if rawValue == nil {
		return 0, fmt.Errorf("value column '%s' contains null value", columnName)
	}

	// Handle different numeric types from KQL (reuse existing logic)
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
			return 0, fmt.Errorf("value column '%s' contains null value", columnName)
		}
		return float64(v.Value), nil
	case value.Real:
		if !v.Valid {
			return 0, fmt.Errorf("value column '%s' contains null value", columnName)
		}
		return v.Value, nil
	case value.Int:
		if !v.Valid {
			return 0, fmt.Errorf("value column '%s' contains null value", columnName)
		}
		return float64(v.Value), nil
	case string:
		// Try to parse string as float
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("value column '%s' contains unparseable string value '%s': %w", columnName, v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("value column '%s' contains unsupported type %T", columnName, rawValue)
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
	case value.DateTime:
		if !v.Valid {
			return time.Time{}, fmt.Errorf("timestamp column '%s' contains null value", t.config.TimestampColumn)
		}
		return v.Value, nil
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

		switch v := rawValue.(type) {
		case string:
			labels[labelColumn] = v
		case value.String:
			if !v.Valid {
				return nil, fmt.Errorf("label column '%s' contains invalid value: %T", labelColumn, rawValue)
			}
			labels[labelColumn] = v.Value
		default:
			// Lables must be string key:value pairs.
			return nil, fmt.Errorf("label column '%s' contains unsupported type %T", labelColumn, rawValue)
		}
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

	// Validate value columns configuration and existence
	if len(t.config.ValueColumns) == 0 {
		return fmt.Errorf("at least one value column must be specified in ValueColumns")
	}

	for _, valueColumn := range t.config.ValueColumns {
		if err := t.validateValueColumn(firstRow, valueColumn); err != nil {
			return fmt.Errorf("invalid value column %q: %w", valueColumn, err)
		}
	}

	// Validate MetricNamePrefix format if provided
	if t.config.MetricNamePrefix != "" {
		normalizedPrefix, err := normalizeColumnName(t.config.MetricNamePrefix)
		if err != nil {
			return fmt.Errorf("MetricNamePrefix %q results in invalid normalized name %q", t.config.MetricNamePrefix, normalizedPrefix)
		}
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
		logger.Warnf("Missing label columns: %v. These columns will be skipped in the transformation.", missingLabelColumns)
	}

	return nil
}

// validateValueColumn validates that a specific value column exists and contains numeric data
func (t *KustoToMetricsTransformer) validateValueColumn(row map[string]any, columnName string) error {
	rawValue, exists := row[columnName]
	if !exists {
		return fmt.Errorf("value column '%s' not found in query results", columnName)
	}

	// Check if value is numeric type
	switch v := rawValue.(type) {
	case float64, float32, int, int32, int64, string:
		// Valid numeric types (string will be validated during parsing)
	case value.Long:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", columnName)
		}
	case value.Real:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", columnName)
		}
	case value.Int:
		if !v.Valid {
			return fmt.Errorf("value column '%s' contains null value", columnName)
		}
	case nil:
		return fmt.Errorf("value column '%s' contains null value", columnName)
	default:
		return fmt.Errorf("value column '%s' contains non-numeric type %T", columnName, rawValue)
	}

	return nil
}
