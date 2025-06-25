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


## Alerting

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
