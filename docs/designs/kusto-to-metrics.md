# Kusto-to-Metrics Integration

## Problem Statement

ADX-Mon currently provides two distinct capabilities:
1. **SummaryRules** - Execute KQL queries on a schedule and ingest results into ADX tables ([`api/v1/summaryrule_types.go`](https://github.com/Azure/adx-mon/blob/main/api/v1/summaryrule_types.go))
2. **OTLP Metrics Exporters** - Export metrics to external observability systems ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go))

However, there's no mechanism to connect these two capabilities. Organizations often need to:
- Transform aggregated ADX data (computed via SummaryRules) into standardized OTLP metrics
- Export these metrics to external systems (Prometheus, DataDog, etc.) at regular intervals
- Create derived metrics from complex KQL aggregations for dashboards and alerting

Currently, users must implement custom solutions outside of ADX-Mon to bridge this gap, leading to:
- Duplicated infrastructure for metric transformation and export
- Inconsistent metric schemas across different teams
- Manual synchronization between SummaryRule execution and metrics export

## Solution Overview

We propose implementing a feature that enables SummaryRule outputs to be automatically transformed and exported as OTLP metrics. This integration will provide a standardized, declarative way to create metrics pipelines from ADX data.

### Key Requirements

1. **Generic Design**: Solution must work with any SummaryRule output, not tied to specific schemas
2. **Standardized Transformation**: Define clear schema requirements for converting KQL results to OTLP metrics
3. **Configurable Export**: Allow flexible configuration of export targets and cadence
4. **Separation of Concerns**: Maintain clean boundaries between data aggregation and metrics export

## Design Approaches

### Approach 1: Extend SummaryRule CRD

Add metrics export configuration directly to the existing `SummaryRuleSpec`:

```go
type SummaryRuleSpec struct {
    Database string            `json:"database"`
    Table    string            `json:"table"`
    Name     string            `json:"name"`
    Body     string            `json:"body"`
    Interval metav1.Duration   `json:"interval"`
    
    // New fields for metrics export
    MetricsExport *MetricsExportConfig `json:"metricsExport,omitempty"`
}

type MetricsExportConfig struct {
    Enabled    bool              `json:"enabled"`
    Exporters  []string          `json:"exporters"`
    Interval   metav1.Duration   `json:"interval,omitempty"`
    Transform  TransformConfig   `json:"transform"`
}
```

**Pros:**
- Single CRD to manage
- Direct coupling ensures consistency
- Simpler for basic use cases

**Cons:**
- Violates single responsibility principle
- Limits reusability (can't export from multiple SummaryRules to same exporter)
- Makes SummaryRule CRD more complex
- Harder to extend with additional export targets

### Approach 2: New MetricsExporter CRD (Recommended)

Create a separate CRD that references SummaryRules and manages metrics export:

```go
// MetricsExporterSpec defines the desired state of MetricsExporter
type MetricsExporterSpec struct {
    // SourceRules defines which SummaryRules to export metrics from
    SourceRules []SummaryRuleRef `json:"sourceRules"`
    
    // Exporters defines the OTLP exporters to send metrics to
    Exporters []string `json:"exporters"`
    
    // Interval defines how often to export metrics
    Interval metav1.Duration `json:"interval"`
    
    // Transform defines how to convert query results to metrics
    Transform TransformConfig `json:"transform"`
}

type SummaryRuleRef struct {
    // Name of the SummaryRule in the same namespace
    Name string `json:"name"`
    
    // Database and Table to query for export
    Database string `json:"database"`
    Table    string `json:"table"`
}

type TransformConfig struct {
    // MetricNameColumn specifies which column contains the metric name
    MetricNameColumn string `json:"metricNameColumn"`
    
    // ValueColumn specifies which column contains the metric value  
    ValueColumn string `json:"valueColumn"`
    
    // TimestampColumn specifies which column contains the timestamp
    TimestampColumn string `json:"timestampColumn"`
    
    // LabelColumns specifies columns to use as metric labels
    LabelColumns []string `json:"labelColumns,omitempty"`
    
    // DefaultMetricName provides a fallback if MetricNameColumn is not specified
    DefaultMetricName string `json:"defaultMetricName,omitempty"`
}
```

**Pros:**
- Clean separation of concerns
- Reusable across multiple SummaryRules
- Easier to extend with new export targets
- Can aggregate metrics from multiple sources
- Follows Kubernetes CRD design patterns

**Cons:**
- Additional CRD to manage
- More complex initial setup

## Recommended Solution: Approach 2

We recommend **Approach 2** (New MetricsExporter CRD) for the following reasons:

1. **Better Architecture**: Separates data aggregation concerns (SummaryRule) from metrics export concerns (MetricsExporter)
2. **Flexibility**: One MetricsExporter can reference multiple SummaryRules, enabling composite metrics
3. **Reusability**: Multiple MetricsExporters can reference the same SummaryRule with different configurations
4. **Extensibility**: Easier to add new export targets and transformation options
5. **Kubernetes Best Practices**: Follows single-responsibility principle for CRDs

## Detailed Implementation

### Standard Schema Requirements

For SummaryRule outputs to be exportable as metrics, the result table must contain:

**Required Columns:**
- `metric_value` (real/double): The numeric value of the metric
- `timestamp` (datetime): When the metric was recorded

**Optional Columns:**
- `metric_name` (string): Name of the metric (if not specified, uses MetricsExporter.Transform.DefaultMetricName)
- Any additional columns can be used as labels via `Transform.LabelColumns`

**Example KQL Query:**
```kql
MyTelemetryTable
| where Timestamp between (_startTime .. _endTime)
| summarize 
    metric_value = avg(ResponseTime),
    timestamp = bin(Timestamp, 5m)
    by ServiceName, Region
| extend metric_name = "service_response_time_avg"
```

### MetricsExporter CRD Example

```yaml
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-metrics-exporter
  namespace: monitoring
spec:
  sourceRules:
    - name: response-time-summary
      database: TelemetryDB
      table: ResponseTimeSummary
    - name: error-rate-summary  
      database: TelemetryDB
      table: ErrorRateSummary
  exporters:
    - prometheus-exporter
    - datadog-exporter
  interval: 5m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Region", "Environment"]
    defaultMetricName: "adx_exported_metric"
```

### Integration with Existing Components

The MetricsExporter will integrate with existing ADX-Mon components:

1. **SummaryRuleTask** ([`ingestor/adx/tasks.go`](https://github.com/Azure/adx-mon/blob/main/ingestor/adx/tasks.go)): Continue executing SummaryRules as normal
2. **OTLP Exporters** ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go)): Reuse existing exporter infrastructure
3. **New MetricsExporterTask**: Query SummaryRule output tables, transform to OTLP metrics, and export

### Execution Flow

1. **SummaryRule Execution**: SummaryRuleTask executes KQL queries on schedule, stores results in tables
2. **Metrics Export Trigger**: MetricsExporterTask runs on its own schedule (independent of SummaryRule cadence)
3. **Data Retrieval**: Query the SummaryRule output tables for data since last export
4. **Transformation**: Convert query results to OTLP metrics according to Transform configuration
5. **Export**: Send metrics to configured OTLP exporters

### Validation and Error Handling

The MetricsExporter controller will validate:
- Referenced SummaryRules exist and are in Ready state
- Output tables contain required columns (metric_value, timestamp)
- Specified label columns exist in the output table
- OTLP exporters are configured and available

## Use Cases

### Use Case 1: Service Performance Metrics

```yaml
# SummaryRule for response time aggregation
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: service-response-times
spec:
  database: TelemetryDB
  name: ServiceResponseTimes
  table: ServiceResponseTimeSummary
  interval: 5m
  body: |
    ServiceTelemetry
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = avg(ResponseTimeMs),
        timestamp = bin(Timestamp, 1m)
        by ServiceName, Environment
    | extend metric_name = "service_response_time_avg"

---
# MetricsExporter for Prometheus
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-metrics-to-prometheus
spec:
  sourceRules:
    - name: service-response-times
      database: TelemetryDB
      table: ServiceResponseTimeSummary
  exporters: ["prometheus"]
  interval: 1m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Environment"]
```

### Use Case 2: Multi-Source Dashboard Metrics

```yaml
# Export from multiple SummaryRules to create a comprehensive dashboard
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: dashboard-metrics
spec:
  sourceRules:
    - name: cpu-utilization-summary
      database: InfraDB
      table: CPUUtilizationSummary
    - name: memory-usage-summary
      database: InfraDB
      table: MemoryUsageSummary
    - name: disk-io-summary
      database: InfraDB
      table: DiskIOSummary
  exporters: ["datadog", "grafana-cloud"]
  interval: 30s
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["NodeName", "ClusterName", "Region"]
```

### Use Case 3: Cross-Cluster Data Aggregation

```yaml
# SummaryRule that imports and aggregates data from multiple clusters
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: global-error-rates
spec:
  database: GlobalMetrics
  name: GlobalErrorRates
  table: GlobalErrorRateSummary
  interval: 10m
  body: |
    union
      cluster('eastus-cluster').TelemetryDB.ErrorEvents,
      cluster('westus-cluster').TelemetryDB.ErrorEvents,
      cluster('europe-cluster').TelemetryDB.ErrorEvents
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = count() * 1.0,
        timestamp = bin(Timestamp, 5m)
        by Region = tostring(split(ClusterName, '-')[0])
    | extend metric_name = "global_error_count"

---
# Export global metrics to central monitoring
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: global-metrics-exporter
spec:
  sourceRules:
    - name: global-error-rates
      database: GlobalMetrics
      table: GlobalErrorRateSummary
  exporters: ["central-prometheus"]
  interval: 2m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["Region"]
```

## Implementation Timeline

### Phase 1: Core CRD and Controller
- Define MetricsExporter CRD in `api/v1/metricsexporter_types.go`
- Implement MetricsExporter controller
- Add MetricsExporterTask to ingestor package
- Basic validation and error handling

### Phase 2: Integration and Testing
- Integration with existing OTLP exporters
- Comprehensive test suite
- Documentation and examples
- Performance optimization

### Phase 3: Advanced Features
- Support for custom transformation functions
- Metrics aggregation across time windows
- Dead letter queues for failed exports
- Monitoring and observability for the export pipeline itself

## Backwards Compatibility

This feature is entirely additive and maintains full backwards compatibility:
- Existing SummaryRules continue to work unchanged
- No modifications to existing OTLP exporter configurations
- New MetricsExporter CRD is optional and independent

## Security Considerations

- MetricsExporter will reuse existing RBAC for SummaryRule access
- Export credentials managed through existing OTLP exporter configuration
- Data transformation happens in-memory without persistent storage of intermediate results
- Audit logging for metrics export operations

## Alternative Considerations

### Custom Resource vs ConfigMap
We chose a CRD over ConfigMap for:
- Better validation and schema enforcement
- Native Kubernetes controller patterns
- Status reporting and condition management
- Integration with existing ADX-Mon CRD ecosystem

### Pull vs Push Model
We chose a push model (MetricsExporter actively queries and exports) over pull model because:
- Consistent with existing SummaryRule execution pattern
- Better control over export timing and retry logic
- Easier to implement transformation and batching
- Aligns with OTLP exporter design patterns

This design provides a robust, extensible foundation for connecting ADX-Mon's data aggregation capabilities with external metrics systems while maintaining clean architectural boundaries and following Kubernetes best practices.
