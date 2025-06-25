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
    // SummaryRuleSelector defines which SummaryRules to export metrics from
    // Uses label selectors to automatically discover SummaryRules based on labels,
    // eliminating the need to manually maintain lists of rule names
    SummaryRuleSelector *metav1.LabelSelector `json:"summaryRuleSelector"`
    
    // Exporters defines the OTLP exporters to send metrics to
    Exporters []string `json:"exporters"`
    
    // Interval defines how often to export metrics
    Interval metav1.Duration `json:"interval"`
    
    // Transform defines how to convert query results to metrics
    Transform TransformConfig `json:"transform"`
}

type SummaryRuleRef struct {
    // LabelSelector to automatically discover SummaryRules to export from
    // This enables dynamic discovery of SummaryRules based on common labeling,
    // eliminating the need to manually maintain lists of rule names
    LabelSelector *metav1.LabelSelector `json:"labelSelector"`
    
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
- **Dynamic Discovery**: Automatically includes new SummaryRules based on labels without manual configuration
- **Scalable Management**: Teams can add SummaryRules with appropriate labels and have them automatically exported
- Easier to extend with new export targets
- Can aggregate metrics from multiple sources
- Follows Kubernetes CRD design patterns
- **Reduced Operational Overhead**: No need to update MetricsExporter when adding/removing SummaryRules

**Cons:**
- Additional CRD to manage
- More complex initial setup

## Recommended Solution: Approach 2

We recommend **Approach 2** (New MetricsExporter CRD) for the following reasons:

1. **Better Architecture**: Separates data aggregation concerns (SummaryRule) from metrics export concerns (MetricsExporter)
2. **Dynamic Discovery**: Label selectors enable automatic inclusion of SummaryRules without manual maintenance
3. **Flexibility**: One MetricsExporter can reference multiple SummaryRules through flexible label matching
4. **Reusability**: Multiple MetricsExporters can reference the same SummaryRules with different label selections
5. **Extensibility**: Easier to add new export targets and transformation options
6. **Kubernetes Best Practices**: Follows single-responsibility principle and uses standard label selector patterns
7. **Operational Efficiency**: Teams can independently manage SummaryRules and MetricsExporters through consistent labeling

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

#### Complete OTLP Output Example

Building on the **customer-analytics** example above, here's what the complete OTLP metric output would look like when exported by the MetricsExporter:

**Sample KQL Result Row:**
```
Value: 0.987
LocationId: "datacenter-west"
CustomerResourceId: "customer-abc123"
ServiceTier: "Premium"
Numerator: 2456
Denominator: 2489
StartTimeUTC: 2025-06-25T10:00:00Z
EndTimeUTC: 2025-06-25T10:15:00Z
AvgLatency: 145.3
metric_name: "customer_success_rate_premium"
```

**Complete OTLP Metrics Payload:**
```json
{
  "resourceMetrics": [{
    "resource": {
      "attributes": [
        {
          "key": "service.name",
          "value": {
            "stringValue": "adx-mon-metrics-exporter"
          }
        },
        {
          "key": "service.version", 
          "value": {
            "stringValue": "1.0.0"
          }
        }
      ]
    },
    "scopeMetrics": [{
      "scope": {
        "name": "adx-mon.azure.com/metrics-exporter",
        "version": "v1"
      },
      "metrics": [{
        "name": "customer_success_rate_premium",
        "description": "Customer success rate analytics exported from ADX",
        "unit": "1",
        "gauge": {
          "dataPoints": [{
            "attributes": [
              {
                "key": "LocationId",
                "value": {
                  "stringValue": "datacenter-west"
                }
              },
              {
                "key": "CustomerResourceId", 
                "value": {
                  "stringValue": "customer-abc123"
                }
              },
              {
                "key": "ServiceTier",
                "value": {
                  "stringValue": "Premium"
                }
              },
              {
                "key": "Numerator",
                "value": {
                  "stringValue": "2456"
                }
              },
              {
                "key": "Denominator", 
                "value": {
                  "stringValue": "2489"
                }
              },
              {
                "key": "EndTimeUTC",
                "value": {
                  "stringValue": "2025-06-25T10:15:00Z"
                }
              },
              {
                "key": "AvgLatency",
                "value": {
                  "stringValue": "145.3"
                }
              }
            ],
            "timeUnixNano": "1719403200000000000",
            "asDouble": 0.987
          }]
        }
      }]
    }]
  }]
}
```

**Key Transformation Points:**

1. **Dynamic Metric Naming**: The `metric_name` column value `"customer_success_rate_premium"` becomes the OTLP metric name, enabling flexible naming strategies per service tier
2. **Primary Value Mapping**: The `Value` column (0.987) maps directly to `asDouble` in the OTLP data point, representing the 98.7% success rate
3. **Timestamp Conversion**: `StartTimeUTC` is converted from ISO 8601 format to Unix nanoseconds (`timeUnixNano`)
4. **Rich Attribute Preservation**: All `labelColumns` become OTLP attributes, including:
   - **Business Dimensions**: `LocationId`, `CustomerResourceId`, `ServiceTier` for filtering and grouping
   - **Supporting Metrics**: `Numerator`, `Denominator` for rate calculations and debugging
   - **Auxiliary Data**: `AvgLatency`, `EndTimeUTC` for additional context and correlation
5. **Type Flexibility**: Numeric values are converted to strings in attributes, while the primary metric value maintains its numeric type
6. **OTLP Standards Compliance**: Full resource and scope metadata for proper observability platform integration

This demonstrates how complex ADX analytics with rich dimensional data translates seamlessly into standardized OTLP metrics suitable for any observability platform (Prometheus, DataDog, Grafana, etc.).

### MetricsExporter CRD Example

```yaml
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-metrics-exporter
  namespace: monitoring
spec:
  summaryRuleSelector:
    matchLabels:
      metrics-export: "enabled"
      team: "platform"
    matchExpressions:
      - key: "metric-type"
        operator: In
        values: ["performance", "availability"]
      - key: "environment"
        operator: NotIn
        values: ["development"]
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

This example demonstrates label selector patterns:
1. **Simple Labels**: Match SummaryRules with `metrics-export: enabled` and `team: platform` labels
2. **Expression Matching**: More complex logic to include rules where `metric-type` is "performance" or "availability" but exclude "development" environment

### Integration with Existing Components

The MetricsExporter will integrate with existing ADX-Mon components:

1. **SummaryRuleTask** ([`ingestor/adx/tasks.go`](https://github.com/Azure/adx-mon/blob/main/ingestor/adx/tasks.go)): Continue executing SummaryRules as normal
2. **OTLP Exporters** ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go)): Reuse existing exporter infrastructure
3. **New MetricsExporterTask**: Query SummaryRule output tables, transform to OTLP metrics, and export

### Execution Flow

1. **SummaryRule Execution**: SummaryRuleTask executes KQL queries on schedule, stores results in tables
2. **Label-Based Discovery**: MetricsExporter controller discovers SummaryRules matching the configured label selector
3. **Metrics Export Trigger**: MetricsExporterTask runs on its own schedule (independent of SummaryRule cadence)
4. **Data Retrieval**: Query the discovered SummaryRule output tables for data since last export
5. **Transformation**: Convert query results to OTLP metrics according to Transform configuration
6. **Export**: Send metrics to configured OTLP exporters

### Validation and Error Handling

The MetricsExporter controller will validate:
- Label selector matches at least one existing SummaryRule in the namespace
- Matched SummaryRules are in Ready state
- SummaryRule output tables contain required columns (metric_value, timestamp)
- Specified label columns exist in the SummaryRule output tables
- OTLP exporters are configured and available

## Use Cases

### Use Case 1: Service Performance Metrics

```yaml
# SummaryRule for response time aggregation with labels for metrics export
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: service-response-times
  labels:
    metrics-export: "enabled"
    team: "platform"
    metric-type: "performance"
    environment: "production"
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
# MetricsExporter that automatically discovers SummaryRules by labels
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-metrics-to-prometheus
spec:
  summaryRuleSelector:
    matchLabels:
      metrics-export: "enabled"
      team: "platform"
      metric-type: "performance"
  exporters: ["prometheus"]
  interval: 1m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Environment"]
```

**Key Benefits of Label Selector Approach:**
- **Automatic Discovery**: When new SummaryRules are created with `metrics-export: enabled` and `team: platform` labels, they are automatically included
- **Team Ownership**: Teams can independently manage their SummaryRules by applying consistent labels
- **Environment Control**: Easy to include/exclude rules based on environment labels
- **No Manual Maintenance**: MetricsExporter configurations don't need updates when SummaryRules are added/removed

### Use Case 2: Advanced Analytics with Rich Metadata

```yaml
# SummaryRule for complex analytics with labels for targeted export
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: customer-analytics
  labels:
    metrics-export: "enabled"
    team: "analytics"
    metric-type: "business"
    data-sensitivity: "customer"
    environment: "production"
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
# MetricsExporter that targets analytics team's business metrics
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: customer-analytics-exporter
spec:
  summaryRuleSelector:
    matchLabels:
      metrics-export: "enabled"
      team: "analytics"
      metric-type: "business"
    matchExpressions:
      - key: "data-sensitivity" 
        operator: In
        values: ["customer", "public"]
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
# Export from multiple SummaryRules discovered by infrastructure team labels
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: dashboard-metrics
spec:
  summaryRuleSelector:
    matchLabels:
      metrics-export: "enabled"
      team: "infrastructure"
    matchExpressions:
      - key: "environment"
        operator: In
        values: ["production", "staging"]
      - key: "metric-type"
        operator: In
        values: ["system", "performance"]
  exporters: ["datadog", "grafana-cloud"]
  interval: 30s
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["NodeName", "ClusterName", "Region"]
```

**Advanced Label Selector Benefits:**
- **Team Boundaries**: Infrastructure team manages their SummaryRules independently
- **Unified Selection**: Single label selector can match multiple SummaryRules across different databases and tables
- **Environment Filtering**: Easy production vs staging separation
- **Metric Type Grouping**: System and performance metrics managed together

### Use Case 4: Cross-Cluster Data Aggregation

```yaml
# SummaryRule that imports and aggregates data from multiple clusters
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: global-error-rates
  labels:
    metrics-export: "enabled"
    team: "sre"
    scope: "global"
    metric-type: "reliability"
    priority: "high"
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
# Export global metrics to central monitoring using SRE team labels
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: global-metrics-exporter
spec:
  summaryRuleSelector:
    matchLabels:
      metrics-export: "enabled"
      team: "sre"
      scope: "global"
    matchExpressions:
      - key: "priority"
        operator: In
        values: ["high", "critical"]
  exporters: ["central-prometheus"]
  interval: 2m
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["Region", "ServiceName", "error_rate"]
```

**Global Monitoring Benefits:**
- **SRE Team Ownership**: Clear ownership through team labels
- **Priority-Based Selection**: Only export high/critical priority metrics
- **Scope-Based Grouping**: Distinguish global vs regional metrics
- **Automatic Inclusion**: New global SummaryRules are automatically discovered

## Label Selector Strategy and Best Practices

### Recommended Labeling Conventions

To maximize the benefits of label-based discovery, teams should adopt consistent labeling conventions for SummaryRules:

#### Core Labels
- **`metrics-export`**: Set to `"enabled"` to indicate the SummaryRule should be considered for metrics export
- **`team`**: Identifies the owning team (e.g., `"platform"`, `"sre"`, `"analytics"`)
- **`environment`**: Environment classification (e.g., `"production"`, `"staging"`, `"development"`)

#### Classification Labels  
- **`metric-type`**: Type of metrics produced (e.g., `"performance"`, `"business"`, `"security"`, `"reliability"`)
- **`priority`**: Importance level (e.g., `"critical"`, `"high"`, `"medium"`, `"low"`)
- **`scope`**: Geographic or logical scope (e.g., `"global"`, `"regional"`, `"cluster"`)

#### Functional Labels
- **`data-sensitivity`**: Data classification (e.g., `"public"`, `"internal"`, `"customer"`, `"confidential"`)
- **`resource-type`**: Resource being monitored (e.g., `"cpu"`, `"memory"`, `"disk"`, `"network"`)
- **`service`**: Service or component name (e.g., `"api-gateway"`, `"database"`, `"cache"`)

### Label Selector Patterns

#### Pattern 1: Team-Based Export
```yaml
summaryRuleSelector:
  matchLabels:
    metrics-export: "enabled"
    team: "platform"
    environment: "production"
```
**Use Case**: Platform team wants to export all their production metrics

#### Pattern 2: Multi-Team Collaboration
```yaml
summaryRuleSelector:
  matchExpressions:
    - key: "metrics-export"
      operator: In
      values: ["enabled"]
    - key: "team"
      operator: In
      values: ["platform", "sre", "infrastructure"]
    - key: "priority"
      operator: In
      values: ["high", "critical"]
```
**Use Case**: Cross-team dashboard showing high-priority metrics from multiple teams

#### Pattern 3: Environment and Type Filtering
```yaml
summaryRuleSelector:
  matchLabels:
    metrics-export: "enabled"
    metric-type: "performance"
  matchExpressions:
    - key: "environment"
      operator: NotIn
      values: ["development", "testing"]
```
**Use Case**: Performance metrics from all non-dev environments

#### Pattern 4: Data Sensitivity Controls
```yaml
summaryRuleSelector:
  matchLabels:
    metrics-export: "enabled"
    team: "analytics"
  matchExpressions:
    - key: "data-sensitivity"
      operator: In
      values: ["public", "internal"]
    - key: "environment"
      operator: In
      values: ["production"]
```
**Use Case**: Analytics team exporting only non-sensitive production data

### Benefits of Consistent Labeling

1. **Autonomous Team Management**: Teams can add/remove SummaryRules without coordinating MetricsExporter changes
2. **Policy Enforcement**: Use labels to enforce data governance and access controls
3. **Organizational Alignment**: Labels reflect organizational structure and responsibilities
4. **Operational Flexibility**: Easy to create different export configurations for different purposes
5. **Scalability**: New teams and services can adopt the labeling convention without central coordination

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
