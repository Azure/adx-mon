# Cookbook

## SummaryRules

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
    | where Environment == "_cluster.environment"
    | where Region == "_cluster.region"
    | where Level == "ERROR"
    | summarize ErrorCount = count() 
      by bin(Timestamp, 15m), Service, Pod
    | extend Environment = "_cluster.environment", Region = "_cluster.region"
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
