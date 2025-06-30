// Package transform demonstrates how to use the KustoToPrometheusTransformer
package transform

import (
	"context"
	"fmt"
	"log"

	"go.opentelemetry.io/otel/metric/noop"
)

// ExampleUsage demonstrates how to use the KustoToPrometheusTransformer
func ExampleUsage() {
	// Example KQL query results (what comes from adxexporter/kusto.go)
	kustoResults := []map[string]interface{}{
		{
			"metric_name": "cpu_usage_percent",
			"value":       85.5,
			"timestamp":   "2023-12-25T12:00:00Z",
			"host":        "server1",
			"service":     "web",
		},
		{
			"metric_name": "memory_usage_percent",
			"value":       72.3,
			"timestamp":   "2023-12-25T12:00:00Z",
			"host":        "server1",
			"service":     "web",
		},
	}

	// Create transform configuration
	config := TransformConfig{
		MetricNameColumn: "metric_name",               // Column containing metric names
		ValueColumn:      "value",                     // Column containing metric values
		TimestampColumn:  "timestamp",                 // Column containing timestamps
		LabelColumns:     []string{"host", "service"}, // Columns to use as labels
	}

	// Create a meter (in real usage, this would come from your OpenTelemetry setup)
	meter := noop.NewMeterProvider().Meter("adx-exporter")

	// Create the transformer
	transformer := NewKustoToPrometheusTransformer(config, meter)

	// Validate the configuration against the query results
	if err := transformer.Validate(kustoResults); err != nil {
		log.Fatalf("Configuration validation failed: %v", err)
	}

	// Transform KQL results to Prometheus metrics
	metrics, err := transformer.Transform(kustoResults)
	if err != nil {
		log.Fatalf("Transformation failed: %v", err)
	}

	// Register metrics with OpenTelemetry
	ctx := context.Background()
	if err := transformer.RegisterMetrics(ctx, metrics); err != nil {
		log.Fatalf("Metric registration failed: %v", err)
	}

	// Print the transformed metrics for demonstration
	for i, metric := range metrics {
		fmt.Printf("Metric %d:\n", i+1)
		fmt.Printf("  Name: %s\n", metric.Name)
		fmt.Printf("  Value: %f\n", metric.Value)
		fmt.Printf("  Timestamp: %s\n", metric.Timestamp.Format("2006-01-02T15:04:05Z"))
		fmt.Printf("  Labels:\n")
		for key, value := range metric.Labels {
			fmt.Printf("    %s: %s\n", key, value)
		}
		fmt.Println()
	}
}

// ExampleWithDefaultMetricName shows usage with a single metric name
func ExampleWithDefaultMetricName() {
	// KQL results without a metric name column
	kustoResults := []map[string]interface{}{
		{
			"cpu_percent": 85.5,
			"host":        "server1",
		},
		{
			"cpu_percent": 92.1,
			"host":        "server2",
		},
	}

	// Configuration using default metric name
	config := TransformConfig{
		DefaultMetricName: "cpu_usage_percent", // All rows will use this metric name
		ValueColumn:       "cpu_percent",       // Column containing the values
		LabelColumns:      []string{"host"},    // Host as a label
	}

	meter := noop.NewMeterProvider().Meter("adx-exporter")
	transformer := NewKustoToPrometheusTransformer(config, meter)

	metrics, err := transformer.Transform(kustoResults)
	if err != nil {
		log.Fatalf("Transformation failed: %v", err)
	}

	fmt.Printf("Transformed %d metrics with default name '%s'\n", len(metrics), config.DefaultMetricName)
}
