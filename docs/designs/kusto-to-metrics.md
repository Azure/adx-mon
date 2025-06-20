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

For SummaryRule outputs to be exportable as metrics, the result table must contain columns that can be mapped to the OTLP metrics format. The transformation is highly flexible and supports both simple and complex schemas.

#### Core OTLP Mapping

The MetricsExporter transforms Kusto query results to OTLP metrics using this mapping:

| Kusto Column | OTLP Field | Purpose |
|--------------|------------|---------|
| Configured via `valueColumn` | `NumberDataPoint.Value` | The numeric metric value |
| Configured via `timestampColumn` | `NumberDataPoint.TimeUnixNano` | Metric timestamp |
| Configured via `metricNameColumn` | `Metric.Name` | Metric name identifier |
| Any columns in `labelColumns` | `NumberDataPoint.Attributes[]` | Dimensional labels/metadata |

#### Required Columns
- **Value Column**: Must contain numeric data (real/double/int)
- **Timestamp Column**: Must contain datetime data

#### Optional Columns
- **Metric Name Column**: If not specified, uses `Transform.DefaultMetricName`
- **Label Columns**: Any additional columns become OTLP attributes

#### Simple Example - Generic Use Case

**KQL Query:**
```kql
MyTelemetryTable
| where Timestamp between (_startTime .. _endTime)
| summarize 
    metric_value = avg(ResponseTime),
    timestamp = bin(Timestamp, 5m)
    by ServiceName, Region
| extend metric_name = "service_response_time_avg"
```

**Transform Configuration:**
```yaml
transform:
  metricNameColumn: "metric_name"
  valueColumn: "metric_value"
  timestampColumn: "timestamp"
  labelColumns: ["ServiceName", "Region"]
```

**Resulting OTLP Metric:**
```json
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
```

#### Complex Example - Advanced Analytics Use Case

For more complex schemas with additional metadata and calculated metrics:

**KQL Query:**
```kql
AnalyticsData
| where EventTime between (_startTime .. _endTime)
| summarize 
    Value = avg(SuccessRate),
    Numerator = sum(SuccessCount),
    Denominator = sum(TotalCount),
    StartTimeUTC = min(EventTime),
    EndTimeUTC = max(EventTime)
    by LocationId, CustomerResourceId
| extend metric_name = "success_rate_analytics"
```

**Transform Configuration:**
```yaml
transform:
  metricNameColumn: "metric_name" 
  valueColumn: "Value"
  timestampColumn: "StartTimeUTC"
  labelColumns: ["LocationId", "CustomerResourceId", "Numerator", "Denominator", "EndTimeUTC"]
```

**Resulting OTLP Metric:**
```json
{
  "name": "success_rate_analytics",
  "gauge": {
    "dataPoints": [{
      "value": 0.987,
      "timeUnixNano": "1640995200000000000",
      "attributes": [
        {"key": "LocationId", "value": "datacenter-01"},
        {"key": "CustomerResourceId", "value": "customer-12345"},
        {"key": "Numerator", "value": "1974"},
        {"key": "Denominator", "value": "2000"},
        {"key": "EndTimeUTC", "value": "2022-01-01T10:05:00Z"}
      ]
    }]
  }
}
```

This approach allows any Kusto query result to be transformed into OTLP metrics by:
1. **Selecting which column contains the primary metric value**
2. **Choosing the timestamp column for temporal alignment**  
3. **Mapping all other relevant columns as dimensional attributes**
4. **Optionally specifying a dynamic or static metric name**

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

### Use Case 2: Advanced Analytics with Rich Metadata

```yaml
# SummaryRule for complex analytics with multiple dimensions
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: customer-analytics
spec:
  database: AnalyticsDB
  name: CustomerPerformanceAnalytics
  table: CustomerAnalyticsSummary
  interval: 15m
  body: |
    CustomerEvents
    | where EventTime between (_startTime .. _endTime)
    | summarize 
        Value = avg(SuccessRate),
        Numerator = sum(SuccessfulRequests),
        Denominator = sum(TotalRequests),
        StartTimeUTC = min(EventTime),
        EndTimeUTC = max(EventTime),
        AvgLatency = avg(LatencyMs)
        by LocationId, CustomerResourceId, ServiceTier
    | extend metric_name = strcat("customer_success_rate_", tolower(ServiceTier))

---
# MetricsExporter that maps rich metadata to OTLP attributes
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: customer-analytics-exporter
spec:
  sourceRules:
    - name: customer-analytics
      database: AnalyticsDB
      table: CustomerAnalyticsSummary
  exporters: ["datadog", "custom-analytics-endpoint"]
  interval: 5m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "Value"
    timestampColumn: "StartTimeUTC"
    labelColumns: ["LocationId", "CustomerResourceId", "ServiceTier", "Numerator", "Denominator", "EndTimeUTC", "AvgLatency"]
```

**Resulting OTLP metrics will contain:**
- **Primary value**: Success rate percentage
- **Rich attributes**: Location, customer, service tier, raw counts, time ranges, and auxiliary metrics
- **Flexible naming**: Dynamic metric names based on service tier

### Use Case 3: Multi-Source Dashboard Metrics

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

### Use Case 4: Cross-Cluster Data Aggregation

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
        timestamp = bin(Timestamp, 5m),
        error_rate = count() * 100.0 / countif(isnotempty(SuccessEvent))
        by Region = tostring(split(ClusterName, '-')[0]), ServiceName
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
    labelColumns: ["Region", "ServiceName", "error_rate"]
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
