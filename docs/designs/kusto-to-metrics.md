# Kusto-to-Metrics Integration

## Problem Statement

ADX-Mon currently provides two distinct capabilities:
1. **SummaryRules** - Execute KQL queries on a schedule and ingest results into ADX tables ([`api/v1/summaryrule_types.go`](https://github.com/Azure/adx-mon/blob/main/api/v1/summaryrule_types.go))
2. **OTLP Metrics Exporters** - Export metrics to external observability systems ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go))

However, there's no direct mechanism to execute KQL queries and export the results as OTLP metrics. Organizations often need to:
- Execute KQL queries on ADX data and export results as standardized OTLP metrics
- Export these metrics to external systems (Prometheus, DataDog, etc.) at regular intervals
- Create derived metrics from complex KQL aggregations for dashboards and alerting

Currently, users must either:
- Use SummaryRules to materialize data in ADX tables, then build custom exporters to transform and export
- Implement entirely separate infrastructure outside of ADX-Mon

This leads to:
- Duplicated infrastructure for metric transformation and export
- Inconsistent metric schemas across different teams
- Additional storage costs for intermediate table materialization
- Complex multi-step pipelines that are difficult to maintain

## Solution Overview

We propose implementing a `MetricsExporter` CRD that functions similarly to `SummaryRule` but outputs directly to OTLP exporters instead of ADX tables. This provides a streamlined, declarative way to create KQL-to-OTLP metrics pipelines without intermediate storage.

### Key Requirements

1. **Direct KQL Execution**: Execute KQL queries directly without requiring intermediate table storage
2. **SummaryRule-like Behavior**: Share time management, scheduling, and backlog functionality with SummaryRule
3. **Standardized Transformation**: Define clear schema requirements for converting KQL results to OTLP metrics
4. **Single Exporter Focus**: Target one OTLP exporter per MetricsExporter for simplicity
5. **Synchronous Operation**: Leverage synchronous OTLP export with backlog management for reliability

## Design Approach

### MetricsExporter CRD

The `MetricsExporter` CRD functions like `SummaryRule` but outputs to OTLP exporters instead of ADX tables. It shares core functionality with `SummaryRule` including scheduling, time management, and backlog handling.

```go
// MetricsExporterSpec defines the desired state of MetricsExporter
type MetricsExporterSpec struct {
    // Database is the name of the database to query
    Database string `json:"database"`
    
    // Body is the KQL query to execute
    Body string `json:"body"`
    
    // Interval defines how often to execute the query and export metrics
    Interval metav1.Duration `json:"interval"`
    
    // Exporter defines the OTLP exporter to send metrics to (references named exporter)
    Exporter string `json:"exporter"`
    
    // Transform defines how to convert query results to metrics
    Transform TransformConfig `json:"transform"`
    
    // Criteria for cluster-based execution selection (same as SummaryRule)
    Criteria map[string][]string `json:"criteria,omitempty"`
}

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
```

### Key Design Principles

1. **SummaryRule Consistency**: MetricsExporter shares core behavioral patterns with SummaryRule:
   - Time-based execution windows with `NextExecutionWindow()`
   - Scheduling logic with `ShouldSubmitRule()` equivalent
   - Backlog management for failed exports
   - Status condition tracking
   - Criteria-based cluster selection

2. **Synchronous Export**: Unlike SummaryRule's async ADX operations, OTLP export is synchronous:
   - No async operation tracking needed
   - Simpler status management using primary condition
   - Immediate success/failure feedback
   - Backlog used only for retry scenarios

3. **Single Exporter Focus**: Each MetricsExporter targets one named OTLP exporter:
   - References exporter configured in `collector/export/metric_otlp.go`
   - Simpler configuration and error handling
   - Clear ownership and responsibility boundaries

4. **Direct KQL Execution**: No intermediate ADX table storage:
   - Execute KQL queries directly against source data
   - Transform results in-memory to OTLP format
   - Reduced storage costs and latency

## Detailed Implementation

### Shared Infrastructure with SummaryRule

MetricsExporter leverages proven patterns from SummaryRule implementation:

| Component | SummaryRule | MetricsExporter |
|-----------|-------------|-----------------|
| **Spec Fields** | `Database`, `Body`, `Interval`, `Criteria` | Same core fields + `Exporter`, `Transform` |
| **Status Management** | Conditions with async operation tracking | Simplified condition tracking (synchronous) |
| **Time Management** | `NextExecutionWindow()`, `ShouldSubmitRule()` | Shared time management logic |
| **Backlog Handling** | Async operation queue management | Export retry queue management |
| **Task Execution** | `SummaryRuleTask` | `MetricsExporterTask` (new, shared patterns) |
| **Cluster Selection** | `Criteria` matching | Same criteria-based selection |

### Status Condition Strategy

MetricsExporter uses simplified status tracking compared to SummaryRule:

```go
const (
    // MetricsExporterOwner is the primary condition type
    MetricsExporterOwner = "metricsexporter.adx-mon.azure.com"
)

// No separate LastSuccessfulExecution tracking needed - 
// primary condition's LastTransitionTime serves this purpose for synchronous operations
```

### Standard Schema Requirements

For MetricsExporter KQL queries to produce valid OTLP metrics, the result table must contain columns that can be mapped to the OTLP metrics format. The transformation is highly flexible and supports both simple and complex schemas.

#### Core OTLP Mapping

The MetricsExporter transforms KQL query results to OTLP metrics using this mapping:

| KQL Column | OTLP Field | Purpose |
|------------|------------|---------|
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

This approach allows any KQL query result to be transformed into OTLP metrics by:
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
  name: service-response-times
  namespace: monitoring
spec:
  database: TelemetryDB
  exporter: prometheus-exporter
  interval: 5m
  criteria:
    region: ["eastus", "westus"]
  body: |
    ServiceTelemetry
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = avg(ResponseTimeMs),
        timestamp = bin(Timestamp, 1m)
        by ServiceName, Environment
    | extend metric_name = "service_response_time_avg"
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Environment"]
```

This example demonstrates:
1. **Direct KQL Execution**: Query executes directly without intermediate table storage
2. **Criteria-Based Selection**: Only runs on ingestors with `region` labels matching "eastus" or "westus"
3. **Single Exporter Target**: Exports to the named "prometheus-exporter" configured in collector
4. **Time Window Parameters**: KQL query uses `_startTime` and `_endTime` parameters (same as SummaryRule)

### Integration with Existing Components

The MetricsExporter integrates with existing ADX-Mon components:

1. **OTLP Exporters** ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go)): References named exporters configured in collector
2. **Shared Infrastructure**: Leverages time management and backlog patterns from SummaryRule
3. **New MetricsExporterTask**: Executes KQL queries, transforms results, and exports to OTLP

### Execution Flow

1. **MetricsExporter Scheduling**: MetricsExporterTask determines when to execute based on interval and last execution time
2. **Time Window Calculation**: Calculate execution window using shared logic from SummaryRule (`NextExecutionWindow`)
3. **KQL Query Execution**: Execute configured KQL query with `_startTime` and `_endTime` parameters
4. **Data Transformation**: Convert query results to OTLP metrics according to Transform configuration
5. **Synchronous Export**: Send metrics to configured OTLP exporter (with backlog on failure)
6. **Status Update**: Update condition status based on export success/failure

### Validation and Error Handling

The MetricsExporter controller will validate:
- KQL query syntax and database accessibility
- Transform configuration references valid columns in query results
- Named OTLP exporter exists and is accessible
- Criteria matches current cluster labels (if specified)
- Required columns (value, timestamp) are present in query results

## Use Cases

### Use Case 1: Service Performance Metrics

```yaml
# MetricsExporter for service response time monitoring
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-response-times
  namespace: monitoring
spec:
  database: TelemetryDB
  exporter: prometheus-exporter
  interval: 1m
  criteria:
    environment: ["production"]
  body: |
    ServiceTelemetry
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = avg(ResponseTimeMs),
        timestamp = bin(Timestamp, 1m)
        by ServiceName, Environment
    | extend metric_name = "service_response_time_avg"
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["ServiceName", "Environment"]
```

**Key Benefits:**
- **Direct Execution**: No intermediate table storage required
- **Real-time Metrics**: Fresh data exported every minute
- **Environment Isolation**: Only executes on production ingestors
- **Standard Integration**: Works with any Prometheus-compatible system

### Use Case 2: Advanced Analytics with Rich Metadata

```yaml
# MetricsExporter for complex customer analytics
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: customer-analytics
  namespace: analytics
spec:
  database: AnalyticsDB
  exporter: datadog-exporter
  interval: 15m
  criteria:
    team: ["analytics"]
    data-classification: ["customer-approved"]
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
- **Data Governance**: Only executes on ingestors with appropriate data classification

### Use Case 3: Multi-Region Infrastructure Monitoring

```yaml
# MetricsExporter for infrastructure metrics across regions
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: infrastructure-monitoring
  namespace: sre
spec:
  database: InfrastructureDB
  exporter: grafana-cloud-exporter
  interval: 30s
  criteria:
    role: ["infrastructure"]
    region: ["eastus", "westus", "europe"]
  body: |
    SystemMetrics
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = avg(CpuUtilization),
        timestamp = bin(Timestamp, 30s)
        by NodeName, ClusterName, Region
    | extend metric_name = "node_cpu_utilization"
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["NodeName", "ClusterName", "Region"]
```

**Key Benefits:**
- **High-Frequency Monitoring**: 30-second intervals for infrastructure metrics
- **Multi-Region Deployment**: Criteria ensures execution across all infrastructure regions
- **Centralized Dashboards**: Single exporter feeds Grafana Cloud for global visibility
- **SRE Team Focus**: Clear ownership and responsibility boundaries

### Use Case 4: Cross-Cluster Error Rate Monitoring

```yaml
# MetricsExporter for global error rate aggregation
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: global-error-rates
  namespace: sre
spec:
  database: GlobalMetrics
  exporter: central-prometheus-exporter
  interval: 2m
  criteria:
    scope: ["global"]
    priority: ["high", "critical"]
  body: |
    union
      cluster('eastus-cluster').TelemetryDB.ErrorEvents,
      cluster('westus-cluster').TelemetryDB.ErrorEvents,
      cluster('europe-cluster').TelemetryDB.ErrorEvents
    | where Timestamp between (_startTime .. _endTime)
    | summarize 
        metric_value = count() * 1.0,
        timestamp = bin(Timestamp, 1m),
        error_rate = count() * 100.0 / countif(isnotempty(SuccessEvent))
        by Region = tostring(split(ClusterName, '-')[0]), ServiceName
    | extend metric_name = "global_error_count"
  transform:
    metricNameColumn: "metric_name"
    valueColumn: "metric_value"
    timestampColumn: "timestamp"
    labelColumns: ["Region", "ServiceName", "error_rate"]
```

**Global Monitoring Benefits:**
- **Cross-Cluster Aggregation**: Single query across multiple ADX clusters
- **Priority-Based Execution**: Only runs on high/critical priority ingestors
- **Central Monitoring**: Feeds into central Prometheus for global alerting
- **Rich Context**: Includes both raw counts and calculated error rates

## Configuration Strategy and Best Practices

### Exporter Configuration

MetricsExporters reference named OTLP exporters configured in the collector. This provides:

1. **Centralized Configuration**: OTLP exporters defined once in collector configuration
2. **Reusability**: Multiple MetricsExporters can reference the same exporter
3. **Isolation**: Each MetricsExporter targets one exporter for clear ownership
4. **Flexibility**: Easy to change export destinations without modifying MetricsExporter CRDs

### Criteria-Based Deployment

Use the `criteria` field to control where MetricsExporters execute:

#### Example: Environment-Based Execution
```yaml
spec:
  criteria:
    environment: ["production"]
```
**Result**: Only executes on ingestors started with `--cluster-labels=environment=production`

#### Example: Team and Region-Based Execution  
```yaml
spec:
  criteria:
    team: ["analytics", "data-science"]
    region: ["eastus", "westus"]
```
**Result**: Executes on ingestors with both team AND region criteria matching

#### Example: Priority-Based Execution
```yaml
spec:
  criteria:
    priority: ["high", "critical"]
    data-classification: ["approved"]
```
**Result**: Only runs on ingestors approved for high-priority, classified data processing

### Benefits of Criteria-Based Selection

1. **Security Boundaries**: Control data access based on ingestor capabilities
2. **Performance Isolation**: Separate high-frequency exports from standard processing
3. **Geographic Distribution**: Deploy same MetricsExporter across regions with regional execution
4. **Team Ownership**: Teams control their MetricsExporter execution through consistent criteria
5. **Resource Management**: Distribute export load across appropriate ingestor instances

## Implementation Roadmap

This section provides a methodical breakdown of implementing the MetricsExporter feature across multiple PRs, focusing on leveraging existing SummaryRule patterns while adapting for synchronous OTLP export.

### 1. Foundation: MetricsExporter CRD Definition
**Goal**: Establish the core data structures and API types based on SummaryRule patterns
- **Deliverables**:
  - Create `api/v1/metricsexporter_types.go` with complete CRD spec
  - Define `MetricsExporterSpec`, `TransformConfig`, and status types
  - Reuse SummaryRule patterns for time management and status tracking
  - Add deepcopy generation markers and JSON tags
  - Update `api/v1/groupversion_info.go` to register new types
  - Generate CRD manifests using `make generate-crd CMD=update`
- **Testing**: Unit tests for struct validation and JSON marshaling/unmarshaling
- **Acceptance Criteria**: CRD can be applied to cluster and kubectl can describe the schema

### 2. Shared Infrastructure Extraction
**Goal**: Extract reusable components from SummaryRule for shared use
- **Deliverables**:
  - Create shared interfaces for time management (`NextExecutionWindow`, `ShouldSubmitRule` logic)
  - Extract backlog management patterns for retry scenarios
  - Create shared condition management utilities
  - Extract criteria matching logic for cluster selection
  - Design shared status tracking patterns (simplified for synchronous operations)
- **Testing**: Unit tests for shared utilities with both SummaryRule and MetricsExporter scenarios
- **Acceptance Criteria**: Shared infrastructure supports both async (SummaryRule) and sync (MetricsExporter) patterns

### 3. Transform Engine: KQL to OTLP Conversion
**Goal**: Implement the core transformation logic
- **Deliverables**:
  - Create `transform/kusto_to_otlp.go` with transformation engine
  - Implement column mapping (value, timestamp, metric name, labels)
  - Add data type validation and conversion (numeric values, datetime parsing)
  - Handle missing columns and default values
  - Add comprehensive error handling for malformed data
- **Testing**: Extensive unit tests with various KQL result schemas and edge cases
- **Acceptance Criteria**: Transformer can convert any valid KQL result to OTLP metrics

### 4. MetricsExporter Controller and Basic Reconciliation
**Goal**: Create the MetricsExporter controller infrastructure
- **Deliverables**:
  - Generate controller boilerplate in `operator/metricsexporter.go`
  - Implement basic reconciliation loop with simplified status updates
  - Add controller registration to operator main function
  - Implement criteria validation and cluster matching logic
  - Add RBAC permissions for MetricsExporter and exporter access
- **Testing**: Integration tests for controller registration and basic reconciliation
- **Acceptance Criteria**: Controller starts successfully and validates MetricsExporter configurations

### 5. OTLP Exporter Integration and Discovery
**Goal**: Integrate with existing OTLP exporter infrastructure
- **Deliverables**:
  - Extend exporter discovery to support MetricsExporter references
  - Add exporter configuration validation for MetricsExporter
  - Implement connection management and health checking
  - Add metrics for export success/failure rates
  - Handle exporter unavailability scenarios
- **Testing**: Integration tests with mock OTLP endpoints and real exporters
- **Acceptance Criteria**: Can successfully reference and validate named OTLP exporters

### 6. MetricsExporterTask: Execution Engine
**Goal**: Implement the execution task leveraging SummaryRule patterns
- **Deliverables**:
  - Create `ingestor/export/metrics_exporter_task.go`
  - Implement shared time management logic (adapted from SummaryRule)
  - Add KQL query execution with `_startTime`/`_endTime` parameters
  - Implement synchronous export with backlog for failures
  - Add task registration to ingestor service
  - Implement criteria-based execution filtering
- **Testing**: Integration tests for scheduling, execution, and backlog management
- **Acceptance Criteria**: Tasks execute on schedule and handle failures gracefully

### 7. End-to-End Integration and Validation
**Goal**: Complete the integration pipeline with full validation
- **Deliverables**:
  - Wire together all components in the MetricsExporter controller
  - Implement comprehensive validation (schema, exporters, criteria)
  - Add status reporting with simplified condition management
  - Create health checks and readiness probes
  - Add comprehensive logging and observability
- **Testing**: End-to-end tests with real KQL queries and OTLP exporters
- **Acceptance Criteria**: Complete MetricsExporter workflow functions from query execution to metric export

### 8. Error Handling and Resilience
**Goal**: Implement robust error handling adapted from SummaryRule patterns
- **Deliverables**:
  - Add comprehensive error classification (transient vs permanent)
  - Implement backlog-based retry logic for failed exports
  - Add export failure tracking and alerting
  - Create graceful degradation when OTLP exporters are unavailable
  - Implement circuit breaker patterns for export reliability
- **Testing**: Chaos engineering tests with various failure scenarios
- **Acceptance Criteria**: System gracefully handles and recovers from all failure modes

### 9. Performance Optimization and Scalability
**Goal**: Optimize for production workloads
- **Deliverables**:
  - Add connection pooling and query optimization
  - Implement parallel processing for multiple MetricsExporters
  - Add resource usage monitoring and throttling
  - Optimize memory usage for large result sets
  - Add configurable limits and resource constraints
- **Testing**: Load testing with high-volume data and multiple MetricsExporters
- **Acceptance Criteria**: Handles production-scale workloads within resource constraints

### 10. Comprehensive Test Suite
**Goal**: Ensure complete test coverage across all scenarios
- **Deliverables**:
  - Unit tests for all packages with >90% coverage
  - Integration tests for ADX connectivity and OTLP export
  - End-to-end tests covering full workflow scenarios
  - Performance benchmarks and load tests
  - Shared infrastructure tests covering both SummaryRule and MetricsExporter
- **Testing**: Automated test execution in CI/CD pipeline
- **Acceptance Criteria**: All tests pass consistently in CI environment

### 11. Documentation and Examples
**Goal**: Provide comprehensive documentation for users
- **Deliverables**:
  - Update CRD documentation in `docs/crds.md`
  - Create detailed configuration guide with examples
  - Add troubleshooting guide with common issues
  - Document differences and similarities with SummaryRule
  - Update API reference documentation
- **Testing**: Documentation review and validation of all examples
- **Acceptance Criteria**: Users can successfully configure MetricsExporter using documentation

### 12. Observability and Monitoring
**Goal**: Add comprehensive observability for the feature
- **Deliverables**:
  - Add Prometheus metrics for export rates, latency, and errors
  - Implement structured logging with correlation IDs
  - Add distributed tracing for export pipeline
  - Create Grafana dashboards for monitoring
  - Add health check endpoints for load balancers
- **Testing**: Validate all metrics and tracing in staging environment
- **Acceptance Criteria**: Operations team can monitor and troubleshoot MetricsExporter effectively

### Dependencies and Sequencing

**Critical Path**: Steps 1-7 must be completed in order as each builds on the previous
**Parallel Development**: Steps 8-12 can be developed in parallel once step 7 is complete
**Shared Infrastructure**: Step 2 enables both SummaryRule improvements and MetricsExporter functionality
**Milestone Reviews**: Conduct technical review after steps 2, 4, 7, and 10 before proceeding

This roadmap ensures each PR is focused, reviewable, and incrementally builds toward the complete feature while leveraging proven SummaryRule patterns and maintaining system stability throughout the development process.
