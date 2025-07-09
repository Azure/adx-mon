# Kusto-to-Metrics Integration

## Problem Statement

ADX-Mon currently provides two distinct capabilities:
1. **SummaryRules** - Execute KQL queries on a schedule and ingest results into ADX tables ([`api/v1/summaryrule_types.go`](https://github.com/Azure/adx-mon/blob/main/api/v1/summaryrule_types.go))
2. **OTLP Metrics Exporters** - Export metrics to external observability systems ([`collector/export/metric_otlp.go`](https://github.com/Azure/adx-mon/blob/main/collector/export/metric_otlp.go))

However, there's no direct mechanism to execute KQL queries and export the results as metrics to observability platforms. Organizations often need to:
- Execute KQL queries on ADX data and export results as standardized metrics
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

We propose implementing a new `adxexporter` component that processes `MetricsExporter` CRDs to execute KQL queries and export results as metrics. This provides a streamlined, declarative way to create KQL-to-metrics pipelines with two complementary output modes:

1. **Prometheus Scraping Mode** (Phase 1): Execute queries and expose results as Prometheus metrics on `/metrics` endpoint for Collector to scrape
2. **Direct OTLP Push Mode** (Phase 2): Execute queries and push results directly to OTLP endpoints with backlog/retry capabilities

### Key Requirements

1. **Direct KQL Execution**: Execute KQL queries directly without requiring intermediate table storage
2. **SummaryRule-like Behavior**: Share time management, scheduling, and criteria-based execution patterns
3. **Criteria-Based Deployment**: Support cluster-label based filtering for secure, distributed execution
4. **Resilient Operation**: Provide backlog and retry capabilities for reliable metric delivery

## Architecture Overview

The solution introduces a new `adxexporter` component that operates as a Kubernetes controller, watching `MetricsExporter` CRDs and executing their configured KQL queries on schedule.

### Core Components

1. **`adxexporter`** - New standalone component (`cmd/adxexporter`) that:
   - Watches `MetricsExporter` CRDs via Kubernetes API
   - Executes KQL queries against ADX on specified intervals
   - Transforms results to metrics format
   - Outputs metrics via Prometheus scraping or direct OTLP push

2. **`MetricsExporter` CRD** - Kubernetes custom resource defining:
   - KQL query to execute
   - Transform configuration for metric conversion
   - Execution criteria and scheduling
   - Output mode configuration

3. **Integration with Existing Components**:
   - **Collector** discovers and scrapes `adxexporter` instances via pod annotations
   - **Operator** manages `MetricsExporter` CRD lifecycle (optional integration)

### Deployment Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Collector     │    │   adxexporter   │    │      ADX        │
│                 │    │                 │    │   Clusters      │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │                 │
│ │Pod Discovery│◄├────┤ │/metrics     │ │    │                 │
│ │& Scraping   │ │    │ │endpoint     │ │    │                 │
│ └─────────────┘ │    │ └─────────────┘ │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │                 │
│ │OTLP Export  │ │    │ │KQL Query    │◄├────┤                 │
│ │Targets      │ │    │ │Execution    │ │    │                 │
│ └─────────────┘ │    │ └─────────────┘ │    │                 │
└─────────────────┘    │ ┌─────────────┐ │    └─────────────────┘
                       │ │CRD Watch &  │ │
       ┌───────────────┤ │Reconciliation│ │
       │               │ └─────────────┘ │
       │               └─────────────────┘
       │
       ▼
┌─────────────────┐
│  Kubernetes     │
│   API Server    │
│                 │
│ MetricsExporter │
│     CRDs        │
└─────────────────┘
```

### Output Modes

#### Phase 1: Prometheus Scraping Mode
- `adxexporter` exposes `/metrics` endpoint with OpenTelemetry metrics library
- Metrics refreshed per `MetricsExporter.Interval`
- Collector discovers via pod annotations: `adx-mon/scrape: "true"`
- Simple, fast implementation leveraging existing Collector scraping infrastructure

#### Phase 2: Direct OTLP Push Mode  
- `adxexporter` pushes metrics directly to OTLP endpoints
- Failed exports queued in CRD backlog for retry
- Supports historical data backfill capabilities
- More resilient but requires additional backlog management infrastructure

## Design Approach

### adxexporter Component

The `adxexporter` is a new standalone Kubernetes component with its own binary at `cmd/adxexporter`. It functions as a Kubernetes controller that watches `MetricsExporter` CRDs and executes their KQL queries on schedule.

#### Command Line Interface

```bash
adxexporter \
  --cluster-labels="region=eastus,environment=production,team=platform" \
  --otlp-endpoint="http://otel-collector:4317" \
  --web.enable-lifecycle=true \
  --web.listen-address=":8080" \
  --web.telemetry-path="/metrics"
```

**Parameters:**

- **`--cluster-labels`**: Comma-separated key=value pairs defining this instance's cluster identity. Used for criteria-based filtering of MetricsExporter CRDs. This follows the same pattern as the Ingestor component documented in [ADX-Mon Configuration](../config.md).

- **`--otlp-endpoint`**: (Phase 2) OTLP endpoint URL for direct push mode. When specified, enables direct OTLP export with backlog capabilities.

- **`--web.enable-lifecycle`**: (Phase 1) Boolean flag to enable the Prometheus metrics HTTP server. When enabled, exposes query results as Prometheus metrics on the specified address/path for Collector scraping. This follows Prometheus naming conventions.

- **`--web.listen-address`**: Address and port for the Prometheus metrics server (default: ":8080")

- **`--web.telemetry-path`**: HTTP path for metrics endpoint (default: "/metrics")

#### Criteria-Based Execution

Similar to the Ingestor component, `adxexporter` uses cluster labels to determine which `MetricsExporter` CRDs it should process. This enables:

- **Security Boundaries**: Only process MetricsExporters appropriate for this cluster's data classification
- **Geographic Distribution**: Deploy region-specific adxexporter instances
- **Team Isolation**: Separate processing by team ownership
- **Resource Optimization**: Distribute load across appropriate instances

**Example Criteria Matching:**
```yaml
# MetricsExporter CRD
spec:
  criteria:
    region: ["eastus", "westus"]
    environment: ["production"]
    
# adxexporter instance
--cluster-labels="region=eastus,environment=production,team=sre"
# ✅ Matches: region=eastus AND environment=production
```

### MetricsExporter CRD

The `MetricsExporter` CRD defines KQL queries and their transformation to metrics format. It shares core patterns with `SummaryRule` but targets metrics output instead of ADX table ingestion.

```go
// MetricsExporterSpec defines the desired state of MetricsExporter
type MetricsExporterSpec struct {
    // Database is the name of the database to query
    Database string `json:"database"`
    
    // Body is the KQL query to execute
    Body string `json:"body"`
    
    // Interval defines how often to execute the query and refresh metrics
    Interval metav1.Duration `json:"interval"`
    
    // Transform defines how to convert query results to metrics
    Transform TransformConfig `json:"transform"`
    
    // Criteria for cluster-based execution selection (same pattern as SummaryRule)
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

1. **Standalone Operation**: `adxexporter` operates independently from existing ingestor/operator infrastructure, providing clear separation of concerns and deployment flexibility.

2. **Dual Output Strategy**: 
   - **Phase 1 (Prometheus)**: Fast implementation using existing Collector scraping capabilities
   - **Phase 2 (Direct OTLP)**: Enhanced resilience with backlog and retry mechanisms

3. **Criteria-Based Filtering**: Leverages the same cluster-label approach as Ingestor for secure, distributed execution across different environments and teams.

4. **SummaryRule Consistency**: Shares core behavioral patterns including time management, scheduling logic, and KQL query execution patterns.

5. **Cloud-Native Integration**: Seamless discovery via Kubernetes pod annotations and integration with existing Collector infrastructure.

## Detailed Implementation

### Phase 1: Prometheus Scraping Mode

In the initial implementation, `adxexporter` exposes transformed KQL query results as Prometheus metrics on a `/metrics` endpoint.

#### Metrics Exposure Workflow

1. **CRD Discovery**: `adxexporter` watches Kubernetes API for `MetricsExporter` CRDs matching its cluster labels
2. **Query Execution**: Execute KQL queries on specified intervals using `_startTime` and `_endTime` parameters
3. **Metrics Transformation**: Convert query results to Prometheus metrics using OpenTelemetry metrics library
4. **Metrics Registration**: Register/update metrics in the OpenTelemetry metrics registry
5. **HTTP Exposure**: Serve metrics via HTTP endpoint for Collector scraping

#### Collector Integration

The existing Collector component discovers `adxexporter` instances via Kubernetes pod annotations:

```yaml
# adxexporter Deployment/Pod
metadata:
  annotations:
    adx-mon/scrape: "true"
    adx-mon/port: "8080"
    adx-mon/path: "/metrics"
```

The Collector's pod discovery mechanism automatically detects these annotations and adds the `adxexporter` instances to its scraping targets.

#### Limitations of Phase 1

- **Point-in-Time Metrics**: Prometheus metrics represent current state; no historical backfill capability
- **Scraping Dependency**: Relies on Collector's scraping schedule, not direct control over export timing
- **No Retry Logic**: Failed queries result in stale metrics until next successful execution

### Phase 2: Direct OTLP Push Mode

In the enhanced implementation, `adxexporter` can push metrics directly to OTLP endpoints with full backlog and retry capabilities.

#### Enhanced Workflow

1. **CRD Discovery**: Same as Phase 1
2. **Query Execution**: Same as Phase 1
3. **OTLP Transformation**: Convert query results directly to OTLP metrics format
4. **Direct Push**: Send metrics to configured OTLP endpoint
5. **Backlog Management**: Queue failed exports in CRD status for retry
6. **Historical Backfill**: Process backlogged time windows on successful reconnection

#### Backlog Strategy

Unlike Prometheus scraping, direct OTLP push enables sophisticated backlog management:

```go
type MetricsExporterStatus struct {
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    
    // LastSuccessfulExecution tracks the last successfully exported time window
    LastSuccessfulExecution *metav1.Time `json:"lastSuccessfulExecution,omitempty"`
    
    // Backlog contains failed export attempts pending retry
    Backlog []BacklogEntry `json:"backlog,omitempty"`
}

type BacklogEntry struct {
    StartTime metav1.Time `json:"startTime"`
    EndTime   metav1.Time `json:"endTime"`
    Attempts  int         `json:"attempts"`
    LastAttempt metav1.Time `json:"lastAttempt"`
    Error     string      `json:"error,omitempty"`
}
```

### Shared Infrastructure Patterns

`adxexporter` leverages proven patterns from SummaryRule implementation while operating as an independent component:

| Component | SummaryRule | adxexporter |
|-----------|-------------|-------------|
| **Time Management** | `NextExecutionWindow()`, interval-based scheduling | Same patterns, independent implementation |
| **Criteria Matching** | Cluster label filtering | Same logic, different component |
| **KQL Execution** | ADX query with time parameters | Same patterns for query execution |
| **Status Tracking** | CRD conditions and backlog | Similar condition management |
| **Backlog Handling** | Async operation queues | Export retry queues (Phase 2) |

### Component Independence

Unlike the original design that integrated with ingestor infrastructure, `adxexporter` operates independently:

- **Separate Binary**: Own entrypoint at `cmd/adxexporter`
- **Independent Deployment**: Deployed as separate Kubernetes workload
- **Dedicated Configuration**: Own command-line parameters and configuration
- **Isolated Dependencies**: Direct ADX connectivity without shared connection pools

### Standard Schema Requirements

For `MetricsExporter` KQL queries to produce valid metrics, the result table must contain columns that can be mapped to the metrics format. The transformation is highly flexible and supports both simple and complex schemas.

#### Core Metrics Mapping

The `adxexporter` transforms KQL query results to metrics using this mapping:

| KQL Column | Prometheus/OTLP Field | Purpose |
|------------|----------------------|---------|
| Configured via `valueColumn` | Metric value | The numeric metric value |
| Configured via `timestampColumn` | Metric timestamp | Temporal alignment (OTLP mode) |
| Configured via `metricNameColumn` | Metric name | Metric name identifier |
| Any columns in `labelColumns` | Metric labels/attributes | Dimensional metadata |

#### Required Columns
- **Value Column**: Must contain numeric data (real/double/int)
- **Timestamp Column**: Must contain datetime data (used in OTLP mode for temporal accuracy)

#### Optional Columns
- **Metric Name Column**: If not specified, uses `Transform.DefaultMetricName`
- **Label Columns**: Any additional columns become metric labels/attributes

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

**Resulting Prometheus Metric (Phase 1):**
```
# HELP service_response_time_avg Average response time by service and region
# TYPE service_response_time_avg gauge
service_response_time_avg{ServiceName="api-gateway",Region="us-east-1"} 245.7

# HELP service_response_time_avg Average response time by service and region  
# TYPE service_response_time_avg gauge
service_response_time_avg{ServiceName="user-service",Region="us-west-2"} 189.3
```

**Resulting OTLP Metric (Phase 2):**
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

**Resulting Prometheus Metrics (Phase 1):**
```
# HELP success_rate_analytics Success rate analytics by location and customer
# TYPE success_rate_analytics gauge
success_rate_analytics{LocationId="datacenter-01",CustomerResourceId="customer-12345",Numerator="1974",Denominator="2000",EndTimeUTC="2022-01-01T10:05:00Z"} 0.987
```

**Resulting OTLP Metric (Phase 2):**
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

This approach allows any KQL query result to be transformed into metrics by:
1. **Selecting which column contains the primary metric value**
2. **Choosing the timestamp column for temporal alignment (OTLP mode)**  
3. **Mapping all other relevant columns as dimensional labels**
4. **Optionally specifying a dynamic or static metric name**

### MetricsExporter CRD Example

```yaml
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: service-response-times
  namespace: monitoring
spec:
  database: TelemetryDB
  interval: 5m
  criteria:
    region: ["eastus", "westus"]
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

This example demonstrates:
1. **Direct KQL Execution**: Query executes directly without intermediate table storage
2. **Criteria-Based Selection**: Only processed by `adxexporter` instances with matching cluster labels
3. **Flexible Output**: Works with both Prometheus scraping (Phase 1) and OTLP push (Phase 2) modes
4. **Time Window Parameters**: KQL query uses `_startTime` and `_endTime` parameters (same as SummaryRule)

### adxexporter Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: adxexporter
  namespace: adx-mon-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: adxexporter
  template:
    metadata:
      labels:
        app: adxexporter
      annotations:
        # Enable Collector discovery and scraping
        adx-mon/scrape: "true"
        adx-mon/port: "8080"
        adx-mon/path: "/metrics"
    spec:
      containers:
      - name: adxexporter
        image: adx-mon/adxexporter:latest
        args:
        - --cluster-labels=region=eastus,environment=production,team=platform
        - --web.enable-lifecycle=true
        - --web.listen-address=:8080
        - --web.telemetry-path=/metrics
        ports:
        - containerPort: 8080
          name: metrics
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

### Integration with Existing Components

The `adxexporter` component integrates with existing ADX-Mon infrastructure while maintaining independence:

1. **Collector Integration**: 
   - Collector automatically discovers `adxexporter` instances via pod annotations
   - Scrapes `/metrics` endpoint and forwards to configured destinations
   - No additional Collector configuration required

2. **Kubernetes API Integration**:
   - `adxexporter` watches `MetricsExporter` CRDs via standard Kubernetes client-go
   - Leverages existing RBAC and authentication mechanisms
   - Operates within standard Kubernetes security boundaries

3. **ADX Connectivity**:
   - Direct ADX connection using same authentication patterns as other components
   - Independent connection management and pooling
   - Reuses existing ADX client libraries and connection patterns

### Execution Flow

#### Phase 1: Prometheus Scraping Mode

1. **CRD Discovery**: `adxexporter` discovers `MetricsExporter` CRDs matching its cluster labels
2. **Scheduling**: Determines execution windows based on `Interval` and last execution time
3. **Query Execution**: Execute KQL query with `_startTime` and `_endTime` parameters
4. **Metrics Transformation**: Convert query results to Prometheus metrics format
5. **Registry Update**: Update OpenTelemetry metrics registry with new values
6. **HTTP Exposure**: Serve updated metrics on `/metrics` endpoint
7. **Collector Scraping**: Collector discovers and scrapes metrics endpoint
8. **Status Update**: Update CRD status with execution results

#### Phase 2: Direct OTLP Push Mode

1. **CRD Discovery**: Same as Phase 1
2. **Scheduling**: Same as Phase 1, plus backlog processing
3. **Query Execution**: Same as Phase 1
4. **OTLP Transformation**: Convert query results directly to OTLP format
5. **Direct Push**: Send metrics to configured OTLP endpoint
6. **Backlog Management**: Queue failed exports for retry
7. **Status Update**: Update CRD status with execution and backlog state

### Validation and Error Handling

The `adxexporter` controller validates:
- KQL query syntax and database accessibility
- Transform configuration matches query result schema
- Cluster label criteria for CRD processing eligibility
- Required columns (value, timestamp) are present in query results
- OTLP endpoint connectivity (Phase 2)

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
  interval: 1m
  criteria:
    environment: ["production"]
    team: ["platform"]
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

**Deployment Configuration:**
```bash
# adxexporter instance matching criteria
adxexporter \
  --cluster-labels="environment=production,team=platform,region=eastus" \
  --web.enable-lifecycle=true
```

**Key Benefits:**
- **Direct Execution**: No intermediate table storage required
- **Real-time Metrics**: Fresh data exposed every minute via `/metrics` endpoint
- **Environment Isolation**: Only processed by `adxexporter` instances with matching criteria
- **Standard Integration**: Collector automatically discovers and scrapes metrics

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

**Deployment Configuration:**
```bash
# adxexporter instance for analytics team
adxexporter \
  --cluster-labels="team=analytics,data-classification=customer-approved,region=westus" \
  --web.enable-lifecycle=true \
  --otlp-endpoint="http://analytics-otel-collector:4317"  # Phase 2
```

**Resulting Metrics:**
- **Primary value**: Success rate percentage  
- **Rich labels**: Location, customer, service tier, raw counts, time ranges, and auxiliary metrics
- **Flexible naming**: Dynamic metric names based on service tier
- **Data Governance**: Only processed by appropriately classified `adxexporter` instances

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

**Multi-Region Deployment:**
```bash
# East US adxexporter
adxexporter --cluster-labels="role=infrastructure,region=eastus" --web.enable-lifecycle=true

# West US adxexporter  
adxexporter --cluster-labels="role=infrastructure,region=westus" --web.enable-lifecycle=true

# Europe adxexporter
adxexporter --cluster-labels="role=infrastructure,region=europe" --web.enable-lifecycle=true
```

**Key Benefits:**
- **High-Frequency Monitoring**: 30-second metric refresh intervals
- **Geographic Distribution**: Each region processes the same MetricsExporter with regional data
- **Centralized Collection**: All regional `adxexporter` instances scraped by their respective Collectors
- **SRE Team Focus**: Clear ownership through criteria-based filtering

### Use Case 4: Cross-Cluster Error Rate Monitoring with Direct Push

```yaml
# MetricsExporter for global error rate aggregation
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: global-error-rates
  namespace: sre
spec:
  database: GlobalMetrics
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

**Phase 2 Deployment with Direct Push:**
```bash
# Global monitoring adxexporter with OTLP push
adxexporter \
  --cluster-labels="scope=global,priority=high" \
  --otlp-endpoint="http://central-prometheus-gateway:4317" \
  --web.enable-lifecycle=false  # Disable scraping, use direct push only
```

**Global Monitoring Benefits:**
- **Cross-Cluster Aggregation**: Single query across multiple ADX clusters
- **Priority-Based Processing**: Only runs on high/critical priority `adxexporter` instances  
- **Direct Push Reliability**: OTLP endpoint receives metrics with backlog/retry capabilities
- **Rich Context**: Includes both raw counts and calculated error rates in labels

## Configuration Strategy and Best Practices

### adxexporter Configuration

The `adxexporter` component provides flexible configuration for different deployment scenarios:

#### Phase 1: Prometheus Scraping Mode
```bash
adxexporter \
  --cluster-labels="team=platform,environment=production,region=eastus" \
  --web.enable-lifecycle=true \
  --web.listen-address=":8080" \
  --web.telemetry-path="/metrics"
```

#### Phase 2: Direct OTLP Push Mode  
```bash
adxexporter \
  --cluster-labels="team=analytics,data-classification=approved" \
  --otlp-endpoint="http://otel-collector:4317" \
  --web.enable-lifecycle=false
```

#### Hybrid Mode (Both Scraping and Push)
```bash
adxexporter \
  --cluster-labels="team=sre,priority=critical" \
  --web.enable-lifecycle=true \
  --web.listen-address=":8080" \
  --otlp-endpoint="http://central-monitoring:4317"
```

### Criteria-Based Deployment Strategy

Use the `criteria` field in MetricsExporter CRDs to control which `adxexporter` instances process them. This follows the same pattern as documented in [ADX-Mon Ingestor Configuration](../config.md).

#### Example: Environment-Based Processing
```yaml
spec:
  criteria:
    environment: ["production"]
```
**adxexporter Configuration:**
```bash
--cluster-labels="environment=production,region=eastus"
```
**Result**: Only `adxexporter` instances with `environment=production` cluster labels process this MetricsExporter

#### Example: Team and Region-Based Processing  
```yaml
spec:
  criteria:
    team: ["analytics", "data-science"]
    region: ["eastus", "westus"]
```
**adxexporter Configuration:**
```bash
--cluster-labels="team=analytics,region=eastus,data-classification=approved"
```
**Result**: Processes MetricsExporter because team=analytics AND region=eastus both match

#### Example: Data Classification Controls
```yaml
spec:
  criteria:
    data-classification: ["public", "internal"]
    priority: ["high"]
```
**adxexporter Configuration:**
```bash
--cluster-labels="data-classification=internal,priority=high,team=platform"
```
**Result**: Processes MetricsExporter because data-classification=internal AND priority=high both match

### Benefits of Criteria-Based Architecture

1. **Security Boundaries**: Control data access based on `adxexporter` deployment classification
2. **Performance Isolation**: Deploy separate `adxexporter` instances for high-frequency vs. low-frequency metrics
3. **Geographic Distribution**: Regional `adxexporter` instances process region-appropriate MetricsExporters
4. **Team Autonomy**: Teams deploy their own `adxexporter` instances with appropriate cluster labels
5. **Resource Optimization**: Distribute MetricsExporter processing load across appropriate instances

### Collector Integration

The existing Collector component automatically discovers `adxexporter` instances through Kubernetes pod annotations:

```yaml
# Pod annotations for Collector discovery
metadata:
  annotations:
    adx-mon/scrape: "true"        # Enable scraping
    adx-mon/port: "8080"          # Metrics port
    adx-mon/path: "/metrics"      # Metrics path
```

No additional Collector configuration is required - discovery and scraping happen automatically.

## Implementation Roadmap

This section provides a methodical breakdown of implementing the `adxexporter` component and `MetricsExporter` CRD across multiple PRs, with Phase 1 focusing on Prometheus scraping and Phase 2 adding direct OTLP push capabilities.

### 📊 Phase 1 Status Summary

**Overall Progress: 4/7 tasks complete (57%)**

- ✅ **Complete (4 tasks)**: Foundation, Scaffolding, Query Execution, Transform Engine  
- 🔄 **Partially Complete (2 tasks)**: Metrics Server, Status Management  
- ❌ **Not Started (1 task)**: Collector Discovery Integration

**Key Achievements:**
- Complete MetricsExporter CRD with time window management and criteria matching
- Functional adxexporter component with Kubernetes controller framework
- Working KQL query execution with ADX integration and time window management
- Full transform engine for KQL to OpenTelemetry metrics conversion with validation
- OpenTelemetry Prometheus exporter setup and health checks
- Comprehensive unit tests for all core functionality

**Remaining Work for Phase 1:**
- **Task 5**: Start HTTP server to serve `/metrics` endpoint (OpenTelemetry backend is ready)
- **Task 6**: Create Kubernetes deployment manifests with collector discovery annotations  
- **Task 7**: Implement CRD status updates to cluster (methods exist, need cluster integration)
- **Integration**: Add end-to-end tests with Collector discovery and scraping

**Code Quality**: 
- ✅ Extensive unit test coverage
- ✅ Follows ADX-Mon patterns (SummaryRule consistency)
- ✅ Proper error handling and logging
- ✅ Command-line interface with flag parsing

### 🔍 Implementation Details Found

**Files Implemented:**
- `api/v1/metricsexporter_types.go` - Complete CRD definition with time management methods
- `cmd/adxexporter/main.go` - Main component with CLI parsing and manager setup
- `adxexporter/service.go` - Controller reconciler with criteria matching and query execution
- `adxexporter/kusto.go` - KQL query executor with ADX client integration
- `transform/kusto_to_metrics.go` - Transform engine for KQL results to OpenTelemetry metrics
- Generated CRD manifests in `kustomize/bases/` and `operator/manifests/crds/`

**Key Features Working:**
- ✅ MetricsExporter CRD with complete spec (Database, Body, Interval, Transform, Criteria)
- ✅ Time window calculation (`ShouldExecuteQuery`, `NextExecutionWindow`)
- ✅ Cluster-label based criteria matching (case-insensitive)
- ✅ KQL query execution with `_startTime`/`_endTime` substitution
- ✅ Transform validation and column mapping (value, labels, timestamps, metric names)
- ✅ OpenTelemetry Prometheus exporter setup with namespace
- ✅ Controller-runtime manager with graceful shutdown
- ✅ Health checks (readyz/healthz endpoints on port 8081)

**Missing Implementation:**
- HTTP server startup for `/metrics` endpoint (OpenTelemetry backend configured but not served)
- Kubernetes deployment manifests with `adx-mon/scrape` annotations
- CRD status updates to Kubernetes cluster (status methods implemented locally only)

### Phase 1: Prometheus Scraping Implementation

#### 1. Foundation: MetricsExporter CRD Definition ✅ COMPLETE
**Goal**: Establish the core data structures and API types
- **Deliverables**:
  - ✅ Create `api/v1/metricsexporter_types.go` with complete CRD spec
  - ✅ Define `MetricsExporterSpec`, `TransformConfig`, and status types
  - ✅ Add deepcopy generation markers and JSON tags
  - ✅ Update `api/v1/groupversion_info.go` to register new types
  - ✅ Generate CRD manifests using `make generate-crd CMD=update`
- **Testing**: ✅ Unit tests for struct validation and JSON marshaling/unmarshaling
- **Acceptance Criteria**: ✅ CRD can be applied to cluster and kubectl can describe the schema

#### 2. adxexporter Component Scaffolding ✅ COMPLETE
**Goal**: Create the standalone `adxexporter` component infrastructure
- **Deliverables**:
  - ✅ Create `cmd/adxexporter/main.go` with command-line argument parsing
  - ✅ Implement cluster-labels parsing and criteria matching logic
  - ✅ Add Kubernetes client-go setup for CRD watching
  - ✅ Create basic controller framework for MetricsExporter reconciliation
  - ✅ Add graceful shutdown and signal handling
- **Testing**: ✅ Integration tests for component startup and CRD discovery
- **Acceptance Criteria**: ✅ `adxexporter` starts successfully and can discover MetricsExporter CRDs

#### 3. KQL Query Execution Engine ✅ COMPLETE
**Goal**: Implement KQL query execution with time window management
- **Deliverables**:
  - ✅ Create ADX client connection management in `adxexporter`
  - ✅ Implement time window calculation logic (adapted from SummaryRule patterns)
  - ✅ Add KQL query execution with `_startTime`/`_endTime` parameter injection
  - ✅ Implement scheduling logic based on `Interval` and last execution tracking
  - ✅ Add comprehensive error handling for query failures
- **Testing**: ✅ Unit tests with mock ADX responses and integration tests with real ADX
- **Acceptance Criteria**: ✅ Can execute KQL queries on schedule with proper time window management

#### 4. Transform Engine: KQL to Prometheus Metrics ✅ COMPLETE
**Goal**: Transform KQL query results to Prometheus metrics format
- **Deliverables**:
  - ✅ Create `transform/kusto_to_metrics.go` with transformation engine (Note: uses generic name, not prometheus-specific)
  - ✅ Implement column mapping (value, metric name, labels) for Prometheus format
  - ✅ Add data type validation and conversion (numeric values, string labels)
  - ✅ Handle missing columns and default metric names
  - ✅ Integrate with OpenTelemetry metrics library for Prometheus exposition
- **Testing**: ✅ Extensive unit tests with various KQL result schemas and edge cases
- **Acceptance Criteria**: ✅ Can transform any valid KQL result to Prometheus metrics

#### 5. Prometheus Metrics Server 🔄 PARTIALLY COMPLETE
**Goal**: Expose transformed metrics via HTTP endpoint for Collector scraping
- **Deliverables**:
  - ❌ Implement HTTP server with configurable port and path (OpenTelemetry setup complete, missing HTTP server startup)
  - ✅ Integrate OpenTelemetry Prometheus exporter library
  - ✅ Add metrics registry management and lifecycle handling
  - ❌ Implement graceful shutdown of HTTP server
  - ✅ Add health check endpoints for liveness/readiness probes
- **Testing**: ✅ HTTP endpoint tests and Prometheus format validation
- **Acceptance Criteria**: 🔄 Serves valid Prometheus metrics on `/metrics` endpoint (OpenTelemetry backend ready, needs HTTP server to expose endpoint)

#### 6. Collector Discovery Integration ❌ NOT COMPLETE
**Goal**: Enable automatic discovery by existing Collector infrastructure
- **Deliverables**:
  - ❌ Add pod annotation configuration to `adxexporter` deployment manifests
  - ❌ Document Collector integration patterns and discovery mechanism
  - ❌ Create example Kubernetes manifests with proper annotations
  - ❌ Validate end-to-end scraping workflow with Collector
- **Testing**: ❌ End-to-end tests with real Collector scraping `adxexporter` metrics
- **Acceptance Criteria**: ❌ Collector automatically discovers and scrapes `adxexporter` metrics

#### 7. Status Management and Error Handling 🔄 PARTIALLY COMPLETE
**Goal**: Implement comprehensive status tracking and error recovery
- **Deliverables**:
  - 🔄 Add MetricsExporter CRD status updates with condition management (methods implemented, cluster updates missing)
  - ✅ Implement retry logic for transient query failures
  - ✅ Add structured logging with correlation IDs and trace information
  - ✅ Create error classification (transient vs permanent failures)
  - ❌ Add metrics for `adxexporter` operational monitoring
- **Testing**: ❌ Chaos engineering tests with various failure scenarios
- **Acceptance Criteria**: 🔄 Graceful error handling with proper status reporting (logic implemented, needs cluster status updates)

### Phase 2: Direct OTLP Push Implementation

#### 8. OTLP Client Integration and Prometheus Remote Write Support
**Goal**: Add direct push capabilities with multiple protocol support
- **Deliverables**:
  - Integrate OpenTelemetry OTLP exporter client
  - **Leverage `pkg/prompb` for Prometheus remote write support**
  - Add OTLP endpoint configuration and connection management
  - Implement OTLP metrics format transformation (separate from Prometheus)
  - **Add Prometheus remote write transformation using `pkg/prompb.TimeSeries`**
  - Add connection health checking and circuit breaker patterns
  - Support both HTTP and gRPC OTLP protocols
  - **Support Prometheus remote write protocol via `pkg/prompb.WriteRequest`**
- **Testing**: Integration tests with mock and real OTLP endpoints and Prometheus remote write
- **Acceptance Criteria**: Can push metrics directly to OTLP endpoints and Prometheus remote write endpoints

#### 9. Backlog and Retry Infrastructure
**Goal**: Implement sophisticated backlog management for reliable delivery
- **Deliverables**:
  - Extend MetricsExporter CRD status with backlog tracking
  - Implement failed export queuing in CRD status
  - Add exponential backoff retry logic with configurable limits
  - Create backlog processing scheduler for historical data
  - **Leverage `pkg/prompb` pooling mechanisms (`WriteRequestPool`, `TimeSeriesPool`) for memory efficiency**
  - Add dead letter queue for permanently failed exports
- **Testing**: Reliability tests with network partitions and endpoint failures
- **Acceptance Criteria**: Reliable metric delivery with historical backfill capabilities

#### 9.1. Leveraging `pkg/prompb` for Enhanced Performance and Timestamp Fidelity
**Goal**: Utilize existing high-performance protobuf implementation for Phase 2
- **Deliverables**:
  - **Transform Engine Enhancement**: Create `transform/kusto_to_prompb.go` to convert KQL results directly to `pkg/prompb.TimeSeries`
  - **Timestamp Preservation**: Use `pkg/prompb.Sample` to preserve actual timestamps from `TimestampColumn` (unlike Phase 1 gauges)
  - **Memory Optimization**: Implement object pooling using `pkg/prompb.WriteRequestPool` and `pkg/prompb.TimeSeriesPool`
  - **Historical Data Support**: Enable proper temporal ordering for backfill scenarios using `pkg/prompb.Sample.Timestamp`
  - **Efficient Batching**: Group multiple time series into `pkg/prompb.WriteRequest` for batch processing
  - **Label Optimization**: Use `pkg/prompb.Sort()` for proper label ordering and efficient serialization
- **Key Benefits**:
  - **Reduced GC Pressure**: Object pooling minimizes memory allocations during high-frequency processing
  - **Timestamp Fidelity**: Preserve actual query result timestamps instead of current time
  - **Prometheus Compatibility**: Native support for Prometheus remote write protocol
  - **Performance**: Optimized protobuf marshaling for large result sets
  - **Backfill Capability**: Support historical data with proper temporal alignment
- **Testing**: Performance benchmarks comparing pooled vs non-pooled implementations
- **Acceptance Criteria**: Significantly reduced memory allocation and improved timestamp accuracy

#### 10. Hybrid Mode Operation
**Goal**: Support multiple output modes simultaneously with shared query execution
- **Deliverables**:
  - Enable concurrent operation of Prometheus scraping, OTLP push, and Prometheus remote write
  - Add configuration options for selective output mode per MetricsExporter
  - Implement shared query execution with multiple output transformations:
    - **OpenTelemetry metrics** (Phase 1) for `/metrics` endpoint scraping
    - **OTLP format** for direct OTLP push  
    - **`pkg/prompb.TimeSeries`** for Prometheus remote write
  - **Dual Transform Architecture**: Create separate transform paths while sharing KQL execution
  - Add performance optimization for multi-mode operation
- **Testing**: Load tests with all output modes active simultaneously
- **Acceptance Criteria**: Efficient operation in hybrid mode without performance degradation

### Phase 2 Architecture Enhancement: `pkg/prompb` Integration

#### Motivation for `pkg/prompb` Integration

The existing `pkg/prompb` package provides significant advantages for Phase 2 implementation:

1. **Timestamp Fidelity**: Unlike Phase 1 OpenTelemetry gauges (which represent current state), `pkg/prompb.Sample` preserves actual timestamps from KQL `TimestampColumn`
2. **Memory Efficiency**: Object pooling (`WriteRequestPool`, `TimeSeriesPool`) reduces GC pressure during high-frequency processing  
3. **Historical Data Support**: Proper temporal ordering enables backfill scenarios with accurate timestamps
4. **Prometheus Compatibility**: Native support for Prometheus remote write protocol
5. **Performance**: Optimized protobuf marshaling for large result sets

#### Implementation Strategy

**Dual Transform Architecture:**
```go
// Phase 1: OpenTelemetry metrics (current)
func (r *MetricsExporterReconciler) transformToOTelMetrics(rows []map[string]any) ([]transform.MetricData, error)

// Phase 2: Add prompb transformation
func (r *MetricsExporterReconciler) transformToPromTimeSeries(rows []map[string]any) ([]*prompb.TimeSeries, error)
```

**Key Integration Points:**

1. **Transform Engine**: Create `transform/kusto_to_prompb.go` alongside existing `transform/kusto_to_metrics.go`
2. **Memory Management**: Use `prompb.WriteRequestPool.Get()` and `prompb.TimeSeriesPool.Get()` for efficient object reuse
3. **Timestamp Handling**: Extract timestamps from `TimestampColumn` and convert to `int64` for `prompb.Sample.Timestamp`
4. **Label Processing**: Use `prompb.Sort()` for proper label ordering and efficient serialization
5. **Batching**: Group multiple time series into `prompb.WriteRequest` for batch transmission

**Configuration Extensions:**
```go
type MetricsExporterReconciler struct {
    // ... existing fields ...
    PrometheusRemoteWriteEndpoint string
    EnablePrometheusRemoteWrite   bool
    EnableOTLP                    bool
}
```

**Output Mode Selection:**
- **Phase 1 Only**: OpenTelemetry metrics for `/metrics` scraping
- **Phase 2 Hybrid**: OpenTelemetry + Prometheus remote write + OTLP push
- **Phase 2 Direct**: Skip OpenTelemetry, use only push modes for better performance

#### Benefits Over Current Implementation

| Aspect | Phase 1 (OpenTelemetry) | Phase 2 (with prompb) |
|--------|--------------------------|------------------------|
| **Timestamp Handling** | Current time only | Preserves actual query timestamps |
| **Memory Usage** | Standard allocation | Pooled objects, reduced GC pressure |
| **Historical Data** | Not supported | Full backfill capability |
| **Protocol Support** | Prometheus scraping only | Prometheus remote write + OTLP |
| **Performance** | Good for scraping | Optimized for high-volume push |

### Quality and Operations

#### 11. Performance Optimization and Scalability
**Goal**: Optimize for production workloads and multi-tenancy
- **Deliverables**:
  - Add connection pooling and query optimization
  - Implement parallel processing for multiple MetricsExporter CRDs
  - **Leverage `pkg/prompb` pooling for memory-efficient metric processing**
  - Add resource usage monitoring and throttling mechanisms
  - Optimize memory usage for large result sets using pooled objects
  - **Implement efficient label sorting and deduplication using `pkg/prompb.Sort()`**
  - Add configurable resource limits and circuit breakers
- **Testing**: Load testing with high-volume data and many MetricsExporter CRDs
- **Acceptance Criteria**: Handles production-scale workloads within resource constraints

#### 12. Comprehensive Test Suite
**Goal**: Ensure complete test coverage across all scenarios
- **Deliverables**:
  - Unit tests for all packages with >90% coverage
  - Integration tests for ADX connectivity and metrics output
  - End-to-end tests covering full workflow scenarios
  - Performance benchmarks and scalability tests
  - Chaos engineering tests for resilience validation
- **Testing**: Automated test execution in CI/CD pipeline
- **Acceptance Criteria**: All tests pass consistently in CI environment

#### 13. Documentation and Examples
**Goal**: Provide comprehensive documentation for users and operators
- **Deliverables**:
  - Update CRD documentation in `docs/crds.md`
  - Create detailed configuration guide with deployment examples
  - Add troubleshooting guide for common issues and debugging
  - Document best practices for criteria-based deployment
  - Create operational runbooks for production deployment
- **Testing**: Documentation review and validation of all examples
- **Acceptance Criteria**: Users can successfully deploy and operate `adxexporter` using documentation

#### 14. Observability and Monitoring
**Goal**: Add comprehensive observability for operational excellence
- **Deliverables**:
  - Add Prometheus metrics for `adxexporter` operational metrics (query rates, errors, latency)
  - Implement structured logging with correlation IDs and distributed tracing
  - Create Grafana dashboards for `adxexporter` monitoring
  - Add alerting rules for common failure scenarios
  - Add health check endpoints for load balancer integration
- **Testing**: Validate all metrics and observability in staging environment
- **Acceptance Criteria**: Operations team can monitor and troubleshoot `adxexporter` effectively

### Dependencies and Sequencing

**Phase 1 Critical Path**: Steps 1-7 must be completed sequentially for basic functionality
**Phase 2 Critical Path**: Steps 8-10 build on Phase 1 for enhanced capabilities  
**Parallel Development**: Steps 11-14 can be developed in parallel with Phase 2
**Milestone Reviews**: Technical review after steps 3, 7, 10, and 12

**Key Dependencies:**
- Step 2 enables independent `adxexporter` development
- Step 4 provides foundation for both Phase 1 and Phase 2 output modes
- Step 7 completes Phase 1 for production readiness
- Step 10 completes Phase 2 for enhanced reliability

This roadmap ensures incremental delivery with Phase 1 providing immediate value through Prometheus integration, while Phase 2 adds sophisticated reliability features for enterprise deployments.

## Conclusion

The `adxexporter` component and `MetricsExporter` CRD provide a comprehensive solution for transforming ADX data into standardized metrics for observability platforms. The phased approach ensures rapid time-to-value while building toward enterprise-grade reliability:

**Phase 1 Benefits:**
- **Fast Implementation**: Leverages existing Collector scraping infrastructure
- **Immediate Value**: Enables KQL-to-metrics transformation without intermediate storage
- **Cloud-Native**: Kubernetes-native discovery and deployment patterns
- **Criteria-Based Security**: Secure, distributed processing with team and environment isolation

**Phase 2 Enhancements:**
- **Enterprise Reliability**: Backlog management and retry capabilities for guaranteed delivery
- **Historical Backfill**: Process historical data gaps during outages with proper timestamp preservation
- **Direct Integration**: Push metrics directly to OTLP and Prometheus remote write endpoints
- **Hybrid Flexibility**: Support scraping and multiple push modes simultaneously
- **Performance Optimization**: Leverage `pkg/prompb` pooling for memory efficiency and reduced GC pressure
- **Timestamp Fidelity**: Preserve actual query result timestamps instead of current time

**Key Technical Advantage: `pkg/prompb` Integration**

Phase 2 implementation should leverage the existing `pkg/prompb` package for:
- **Memory Efficiency**: Object pooling reduces allocation overhead during high-frequency processing
- **Timestamp Accuracy**: Preserve temporal fidelity from KQL query results for proper historical analysis
- **Protocol Compatibility**: Native Prometheus remote write support alongside OTLP
- **Performance**: Optimized protobuf serialization for large-scale deployments

This design provides a scalable, secure, and maintainable foundation for organizations to operationalize their ADX analytics data across their observability infrastructure with both immediate scraping capabilities and future-ready push architectures.
