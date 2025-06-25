# Summary Rules

## Background

ADX-Mon ingests telemetry into ADX without restrictions on cardinality or dimensionality.  Storing this raw data for long periods of time is expensive and inefficient.  There are times where it would be useful to aggregate this raw data for longer retention, query efficiency or to reduce costs.

Similarly, sometimes there is data in other ADX clusters that would be useful to join with the local telemetry collected by ADX-Mon.  While this data can be queried using `cluster()` functions, sometimes
the cluster is geographically far from the local cluster and this approach is not as performant as having the data locally.  In addition, the remote
cluster may not have the same retention policies as the local cluster which can lead to query issues.

### Proposed Solution

We will define a CRD, `SummaryRule`, that enables a user to define a KQL query, a interval and a destination Table for the results of the query.  The query will be executed on a schedule and the results will be stored in the destination Table.
ADX-Mon will maintain the last execution time and the start and end time of the query.  The start and end time will be passed to the query, similar to `AlertRules`, to ensure consistent results.

## CRD

Our CRD could simply enable a user to specify any arbitrary KQL; however, to prevent admin commands from being executed, we'll instead specify all the possible fields for a Function and construct the KQL scaffolding ourselves.

The CRD definition is as follows:

```yaml
...
```

A sample use is:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: samplefn
spec:
  database: SomeDatabase
  name: HourlyAvg
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h
```

_Ingestor_ would then execute the following

```kql
let _startTime = datetime(...);
let _endTime = datetime(...);
.set-or-append async SomeMetricHourlyAvg <|
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
```

When the query is executed successfully, the `SummaryRule` CRD will be updated with the last execution time and start
and end time used in the query.  These fields will be used to determine the next execution time and interval.

## Conditional Execution with Criteria

SummaryRules support criteria to enable conditional execution based on cluster labels. This allows the same rule to be deployed across multiple environments while only executing where appropriate.

For example, if an ingestor is started with `--cluster-labels=region=eastus,environment=production`, then a SummaryRule with:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: regional-summary
spec:
  database: SomeDatabase
  name: HourlyAvg
  body: |
    SomeMetric
    | where Timestamp between (_startTime .. _endtime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  table: SomeMetricHourlyAvg
  interval: 1h
  criteria:
    region:
      - eastus
      - westus
    environment:
      - production
```

Would execute because the cluster has `region=eastus` (which matches one of the allowed regions) OR `environment=production` (which matches the required environment). The matching logic uses case-insensitive comparison and OR semantics - any single criteria match allows execution.

If no criteria are specified, the rule executes on all ingestor instances regardless of cluster labels.

## Recent Changes

To simplify querying summarized data with recent data, a view can be used to union the summarized data with the
most recent raw data.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: samplefn
spec:
  name: SomeMetricHourlyAvg
  body: |
    let _startTime = table('SomeMetricHourlyAvg') | summarize max(Timestamp); 
    SomeMetric
    | where Timestamp >= _startTime
    | summarize avg(Value) by bin(Timestamp, 1h)
    | union table('SomeMetricHourlyAvg')
  database: SomeDatabase
  table: SomeMetricHourlyAvg
  isView: true
```

Variations of this view pattern can always return the most recent hour of raw data and summarized data thereafter.  The
query performance will remain consistent as data grows.

In some cases, it may be useful to further layer the summarized data with additional summarized data to support daily or
weekly summaraizations.  This data can be incorporated using another `SummaryRule` and amending the view.

## Data Importing

To import data from another cluster, a `SummaryRule` can be defined to import data using the `cluster()` function.

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: importfn
spec:
    database: SomeDatabase
    name: ImportData
    body: |
        cluster('https://remotecluster.kusto.windows.net').SomeDatabase.SomeTable
    table: SomeTable
    interval: 1d
```

This is useful when the remote cluster has different retention policies, data is queried frequently or there is data collected in other system that is useful for reference.  For example, it might
be useful to import data from a remote cluster that has a global view of all telemetry data but you only need a subset of that data.

> **See also:** [CRD Reference](../crds.md) for a summary of all CRDs and links to advanced usage.
