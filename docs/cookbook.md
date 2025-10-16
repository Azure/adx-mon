# Cookbook

## SummaryRules

SummaryRules automate periodic KQL aggregations in Azure Data Explorer (ADX) for data rollups, downsampling, and ETL operations. They execute on precise time intervals and track async operations until completion.

### Key Concepts

**Time Placeholders**: Always include `_startTime` and `_endTime` in your KQL body - these are automatically replaced with the calculated execution window times.

**Cluster Labels**: Use cluster-specific placeholders like `_environment` or `_region` that get replaced with values from the ingestor's `--cluster-labels` configuration.

**Async Operations**: Queries are submitted as ADX async operations (`.set-or-append async`) and tracked until completion. The system handles retries and operation cleanup automatically.

**Continuous Execution**: Rules execute continuously based on their interval, with each execution picking up exactly where the previous one ended (no gaps or overlaps).

### Creating hourly rollups from high-frequency metrics

Use SummaryRules to create downsampled data for long-term storage and faster queries:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: cpu-usage-hourly
spec:
  database: Metrics
  name: CPUUsageHourly
  body: |
    Metrics
    | where Timestamp between (_startTime .. _endTime)
    | where Name == "cpu_usage_percent"
    | summarize AvgCPU = avg(Value), MaxCPU = max(Value), MinCPU = min(Value) 
      by bin(Timestamp, 1h), Pod, Namespace
  table: CPUUsageHourly
  interval: 1h
```

### Environment-specific rules with cluster labels

Create rules that work across different environments using cluster label substitutions:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: error-rate-by-environment
spec:
  database: Logs
  name: ErrorRateByEnvironment
  body: |
    Logs
    | where Timestamp between (_startTime .. _endTime)
    | where Environment == "_environment"
    | where Region == "_region"
    | where Level == "ERROR"
    | summarize ErrorCount = count() 
      by bin(Timestamp, 15m), Service, Pod
    | extend Environment = "_environment", Region = "_region"
  table: ErrorRateByEnvironment
  interval: 15m
```

Deploy with cluster labels:
```bash
# Production environment
./ingestor --cluster-labels=environment=production --cluster-labels=region=eastus ...

# Staging environment  
./ingestor --cluster-labels=environment=staging --cluster-labels=region=westus ...
```

### Cross-table aggregations

Combine data from multiple tables in a single SummaryRule:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: service-health-summary
spec:
  database: Monitoring
  name: ServiceHealthSummary
  body: |
    let metrics = Metrics 
    | where Timestamp between (_startTime .. _endTime);
    | where Name in ("http_requests_total", "http_request_duration_seconds")
    let logs = Logs
    | where Timestamp between (_startTime .. _endTime);
    | where Level in ("ERROR", "WARN") 
    metrics
    | join kind=leftouter (logs | summarize ErrorCount = count() by Service, bin(Timestamp, 5m)) 
      on Service, $left.bin_Timestamp == $right.Timestamp
    | summarize 
        RequestCount = sum(Value),
        AvgDuration = avg(Duration),
        ErrorCount = sum(ErrorCount)
      by bin(Timestamp, 5m), Service
  table: ServiceHealthSummary
  interval: 5m
```

### Using Ingestion Delay for Data Completeness

To account for ingestion delay, summary rules can be configured with the `ingestionDelay` property.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: external-data-summary
spec:
  database: ExternalMetrics
  name: ExternalDataSummary
  body: |
    ExternalMetrics
    | where Timestamp between (_startTime .. _endTime)
    | where Source == "external_api"
    | summarize
        request_count = count(),
        avg_response_time = avg(ResponseTime),
        error_rate = countif(StatusCode >= 400) * 100.0 / count()
      by bin(Timestamp, 15m), Endpoint
  table: ExternalDataSummary
  interval: 15m
  ingestionDelay: 5m
```

**When to use Ingestion Delay:**
- **All Kusto tables have ingestion delay**: Every table in Kusto has some amount of delay between when data is observed and when it becomes queryable. Use ingestion delay to ensure your summary rules process complete data.
- **External data sources**: External data explorer clusters might have longer ingestion delays.

**Measuring ingestion delay for a table:**
Use this query to understand the typical ingestion delay for your data:

```kql
TableName
| where Timestamp > ago(1d)
| project ObservedTimestamp, IngestionTimestamp=ingestion_time()
| extend diff = IngestionTimestamp-ObservedTimestamp
| summarize percentiles(diff, 50, 90, 99, 100)
```

**Recommended minimum delays:**
- **Logs**: At least 15 minutes
- **Metrics**: At least 5 minutes

**Balancing delay vs. latency:**
- Longer delays ensure better data consistency but add latency to summary rule results
- Use Kusto views to union current data with summary data for up-to-date queries

**How it works:**
- The execution window is shifted back by the specified delay before being aligned to the interval boundary
- For example, with `interval: 1h` and `ingestionDelay: 15m`:
  - Current time: 14:05
  - Without delay: Process 13:00-14:00 data
  - With delay: Process 12:00-13:00 data (shifted back by 10 minutes, then aligned to hour boundary)

### Optimizing Ingestion Latency

There are several layers of batching within ADX-Mon that allow tuning of throughput and latency.
The defaults are configured to support ingestion latencies from source to ADX of less than one minute.  Within
ADX, there are batching policies that may need to be adjusted from their defaults which is tuned for
throughput.

By default, ADX will seal a batch after 5 mins, 500 files or 1 GB of data is available.  For metrics and lower
latencies needs, it is recommended to lower this to 30s, 500 files or 1 GB to achieve faster ingest latency.

You can control ADX ingestion batching behavior at the database level using ManagementCommand resources. These policies determine how quickly data becomes available for querying after ingestion by configuring when ADX seals and commits data batches.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: ingestion-batching-policies
  namespace: adx-mon
spec:
  body: |
    .alter-merge database Metrics policy ingestionbatching ```
    {
      "MaximumBatchingTimeSpan" : "00:00:30",
      "MaximumNumberOfItems" : 500,
      "MaximumRawDataSizeMB" : 1024
    }
    ```
    .alter-merge database Logs policy ingestionbatching ```
    {
      "MaximumBatchingTimeSpan" : "00:05:00",
      "MaximumNumberOfItems" : 500,
      "MaximumRawDataSizeMB" : 4096
    }
    ```
```

**Policy Parameters:**
- **MaximumBatchingTimeSpan**: Maximum time to wait before sealing a batch (format: HH:MM:SS)
- **MaximumNumberOfItems**: Maximum number of items (blobs) per batch
- **MaximumRawDataSizeMB**: Maximum uncompressed data size per batch in MB

**Latency vs. Throughput Trade-offs:**
- **Low latency** (30 seconds): Use for real-time dashboards and alerting data that needs to be queryable quickly
- **Higher throughput** (5 minutes): Use for bulk data that doesn't require immediate availability, reduces ingestion overhead
- **Balanced approach**: Start with 5 minutes and adjust based on query patterns and data volume


#### Per-Table Policies

There may be some tables that you need to lower the ingestion latency from the default database policies.  Specific tables
can be ingested more quickly with a table level ingestion policy.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: ManagementCommand
metadata:
  name: ingestion-batching-policies
  namespace: adx-mon
spec:
  body: |
    .alter-merge table MetricsDB.MyTable policy ingestionbatching ```
    {
      "MaximumBatchingTimeSpan" : "00:00:10",
      "MaximumNumberOfItems" : 500,
      "MaximumRawDataSizeMB" : 1024
    }
    ```
```

Lowering ingestion latency incurs higher resource demands on the ADX side which can increase costs under high volume.

**Best Practices:**
- Apply policies during initial cluster setup before heavy ingestion begins
- Use shorter batching times for metrics databases that power real-time dashboards
- Use longer batching times for logs or historical data where query latency is less critical
- Monitor ingestion performance with `.show ingestion failures` and `.show operations` commands
- Consider your SummaryRule `ingestionDelay` settings when tuning batching policies - ensure the delay is longer than the batching timespan
- Test policy changes on non-production databases first to understand the impact on query availability
- Change Metrics database policies to 30s, 500 files and 1GB for < 1min queryability
- Change Logs database policies to 5m, 500 files and 4GB for higher throughput

**Relationship to SummaryRule ingestionDelay:**
- Database ingestion batching policies control when data is sealed and becomes queryable in ADX
- SummaryRule `ingestionDelay` should be set longer than the database's `MaximumBatchingTimeSpan` to ensure data completeness
- Example: If your database uses `MaximumBatchingTimeSpan: 00:05:00`, set SummaryRule `ingestionDelay: 10m` or longer

## Alerting

### Conditional Execution with criteria / criteriaExpression
Apply rules only on certain clusters or environments without duplicating manifests.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: hourly-cpu-prod-public
spec:
  database: Metrics
  name: CPUUsageHourlyProdPublic
  table: CPUUsageHourly
  interval: 1h
  body: |
    Metrics
    | where Timestamp between (_startTime .. _endTime)
    | where Name == "cpu_usage_percent"
    | summarize AvgCPU = avg(Value) by bin(Timestamp, 1h), Pod, Namespace
  criteria:
    region: [eastus, westus]
  criteriaExpression: env == 'prod' && cloud == 'public'
```

Behavior formula: `(criteria empty OR any match) AND (criteriaExpression empty OR expression true)`.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: MetricsExporter
metadata:
  name: core-metrics-eu
spec:
  database: Metrics
  name: CoreMetrics
  query: |
    Metrics
    | where Timestamp between (_startTime .. _endTime)
    | where Region == '_region'
  interval: 5m
  criteriaExpression: region in ['westeurope','northeurope'] && env != 'dev'
```

If the expression fails to parse or evaluate the exporter execution is skipped.

### Detecting rate of change in a metric

### Detecting increase in error messages

## Metrics

### Annotating pods for collection

### Adding static scrape targets

## Logging

### Instrumenting logs for collection

### Monitoring and Troubleshooting SummaryRules

#### Checking Rule Status

Check the status of your SummaryRules using kubectl:

```bash
# List all SummaryRules and their status
kubectl get summaryrules -o wide

# Get detailed status for a specific rule
kubectl describe summaryrule my-summary-rule

# Check the conditions for operational details
kubectl get summaryrule my-summary-rule -o jsonpath='{.status.conditions}' | jq
```

#### Understanding Rule Conditions

SummaryRules use three main condition types:

1. **`summaryrule.adx-mon.azure.com`**: Overall rule status
   - `True`: Rule is healthy and executing successfully
   - `False`: Rule has errors (check message for details)
   - `Unknown`: Rule is processing or has pending operations

2. **`summaryrule.adx-mon.azure.com/OperationId`**: Active async operations
   - Contains JSON array of up to 200 tracked async operations
   - Each operation has OperationId, StartTime, and EndTime

3. **`summaryrule.adx-mon.azure.com/LastSuccessfulExecution`**: Last successful completion time
   - Contains RFC3339Nano timestamp of when the last execution completed successfully
   - Used to calculate the next execution window

#### Common Issues and Solutions

**Rule not executing**: 
- Check that the rule's database matches the ingestor's target database
- Verify the rule condition status is not `False` (indicating previous failure)
- Ensure the interval has elapsed since the last execution

**Query failures**:
- Validate your KQL syntax in ADX directly
- Check that source tables exist and have data in the time range
- Verify cluster label placeholders match your ingestor's `--cluster-labels`

**Missing time ranges**:
- Rules automatically handle gaps - if an execution fails, the next successful execution will process from where it left off
- Check the `LastSuccessfulExecution` condition to see the actual coverage

**Operations stuck in progress**:
- Operations older than 25 hours are automatically cleaned up
- ADX operations can be viewed with `.show operations` in the ADX cluster
- Stuck operations may indicate ADX cluster issues or resource constraints

#### Performance Considerations

**Interval Sizing**: Choose intervals that balance freshness with ADX performance:
- High-frequency data: 5-15 minutes
- Standard metrics: 1 hour  
- Large datasets: Several hours or daily

**Query Optimization**: 
- Use time range filters efficiently: `where Timestamp between (_startTime .. _endTime)`
- Consider table partitioning and indexing in ADX
- Test query performance in ADX before deploying rules

**Concurrency**: The system tracks up to 200 concurrent async operations per rule. For high-frequency rules, ensure operations complete faster than new ones are submitted.

### Best Practices

#### Design for Idempotency
Always design your queries to be idempotent - they should produce the same result if run multiple times on the same data:

```yaml
# Good: Uses summarize which is naturally idempotent
body: |
  Metrics
  | where Timestamp between (_startTime .. _endTime)
  | summarize avg(Value) by bin(Timestamp, 1h), Pod

# Avoid: Direct data copying without aggregation
body: |
  Metrics  
  | where Timestamp between (_startTime .. _endTime)
```

#### Use Appropriate Table Names
Choose descriptive table names that indicate the aggregation level and purpose:

```yaml
# Examples of good table names
table: MetricsHourly           # Hourly rollups of metrics
table: ErrorRate15Min         # 15-minute error rate calculations  
table: DailyResourceUsage     # Daily resource usage summaries
```

#### Mitigate Latency with Views
When using longer ingestion delays for data consistency, create views that combine historical summaries with recent data for up-to-date queries. See the [Function CRD documentation](crds.md#function) for examples of creating views that union historical summary data with recent raw data.

This approach allows you to:
- Use longer ingestion delays (e.g., 30 minutes) for consistent historical data
- Provide up-to-date data through views that union recent raw data
- Balance data consistency with query freshness

#### Environment Separation
Use cluster labels to create environment-agnostic rules:

```yaml
# Rule works across environments
body: |
  Logs
  | where Environment == "_environment"  # Replaced with actual environment
  | where Region == "_region"          # Replaced with actual region
  | summarize ErrorCount = count() by bin(Timestamp, 15m)
```

Deploy with environment-specific labels:
```bash
# Production
./ingestor --cluster-labels=environment=prod --cluster-labels=region=us-east-1

# Staging  
./ingestor --cluster-labels=environment=staging --cluster-labels=region=us-west-2
```

#### Handle Schema Evolution
Design queries that gracefully handle schema changes:

```yaml
body: |
  Metrics
  | where Timestamp between (_startTime .. _endTime)
  | extend Pod = coalesce(tostring(Pod), tostring(PodName), "unknown")  # Handle column renames
  | where isnotempty(Value) and isfinite(Value)                         # Handle data quality
  | summarize avg(Value) by bin(Timestamp, 1h), Pod
```

#### Test Before Deployment
Always test your KQL queries in ADX before creating SummaryRules:

```kql
// Test your query with sample time ranges first
let _startTime = datetime(2024-01-01T12:00:00Z);
let _endTime = datetime(2024-01-01T13:00:00Z);
Metrics
| where Timestamp between (_startTime .. _endTime)
| summarize avg(Value) by bin(Timestamp, 1h)
```


## Alerting
