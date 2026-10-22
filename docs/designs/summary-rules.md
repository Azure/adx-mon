# Summary Rules

## Background

ADX-Mon ingests telemetry into ADX without restrictions on cardinality or dimensionality.  Storing this raw data for long periods of time is expensive and inefficient.  There are times where it would be useful to aggregate this raw data for longer retention, query efficiency or to reduce costs.

Similarly, sometimes there is data in other ADX clusters that would be useful to join with the local telemetry collected by ADX-Mon.  While this data can be queried using `cluster()` functions, sometimes
the cluster is geographically far from the local cluster and this approach is not as performant as having the data locally.  In addition, the remote
cluster may not have the same retention policies as the local cluster which can lead to query issues.

### Proposed Solution

We will define a CRD, `SummaryRule`, that enables a user to define a KQL query, a interval and a destination Table for the results of the query.  The query will be executed on a schedule and the results will be stored in the destination Table.
ADX-Mon will maintain the last execution time and the start and end time of the query.  The start and end time will be passed to the query, similar to `AlertRules`, to ensure consistent results.

> **Best Practice:**
> Always use `between(_startTime .. _endTime)` for time filtering in your summary rule KQL queries. This ensures correct, non-overlapping, and gap-free time windows. The system guarantees that `_endTime` is exclusive by subtracting 1 tick (100ns) from the window, so you can safely use inclusive `between` logic.

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
    | where Timestamp between (_startTime .. _endTime)
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
    | where Timestamp between (_startTime .. _endTime)
    | summarize avg(Value) by bin(Timestamp, 1h)
```

> **Note:**
> The use of `between(_startTime .. _endTime)` is required for correct time windowing. Do not use `>= _startTime and < _endTime` or other variants; the system already ensures no overlap or gap by adjusting `_endTime`.

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
    | where Timestamp between (_startTime .. _endTime)
    | summarize avg(Value)
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
  body: |
    .create-or-alter function with (view=true, folder='views') SomeMetricHourlyAvg () {
      let _interval = 1h;
      let _startTime = toscalar(table('SomeMetricHourlyAvg') | summarize max(Timestamp)); 
      SomeMetric
      | where Timestamp >= _startTime
      | summarize avg(Value) by bin(Timestamp, _interval)
      | union table('SomeMetricHourlyAvg')
    }
  database: SomeDatabase
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

## Historical Backfill

SummaryRules support explicit historical backfill via an optional `backfill` field in the spec. This lets you process
a specific time range that was either never summarized or needs to be re-computed.

### Usage

Add a `backfill` block to an existing SummaryRule:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: hourly-avg
spec:
  database: Metrics
  table: MetricHourlyAvg
  interval: 1h
  body: |
    RawMetric
    | where Timestamp between (_startTime .. _endTime)
    | summarize avg(Value) by bin(Timestamp, 1h)
  backfill:
    requestId: jan-2026        # User-chosen identifier; same ID = resume
    startTime: "2026-01-01T00:00:00Z"  # Inclusive start
    endTime: "2026-02-01T00:00:00Z"    # Exclusive end
    maxInFlight: 1             # Max concurrent async ops (default: 1, max: 20)
```

### Key Semantics

- **`requestId`**: Required. The same `requestId` resumes an in-progress backfill. A new `requestId` starts fresh.
- **Separate cursor**: Backfill uses its own progress cursor (`status.backfill.nextWindowStart`) and does not
  modify `LastSuccessfulExecution` used by normal scheduling.
- **No skipped intervals**: Retryable async failures are automatically re-queued for retry. Non-retryable
  failures stop the backfill and mark it `Failed` rather than silently skipping a window.
- **No overlapping intervals**: Windows are generated sequentially from `startTime`, advancing by exactly one
  `interval` each time. A deduplication guard prevents double-submission.
- **Whole-interval ranges only**: `endTime - startTime` must cover one or more whole `interval` windows.
  Partial trailing windows are rejected up front instead of being silently dropped.
- **Generation pinning**: If the SummaryRule spec is edited mid-backfill (changing body, interval, etc.), the
  backfill is failed and a new `requestId` must be submitted.
- **Low priority**: Backfill is designed as a background task. With `maxInFlight: 1` (default), only one
  window is in-flight at a time. `maxInFlight` is capped at 20 to keep historical processing throttled.
  A complete backfill may take days for large ranges — this is by design.
- **Append-only**: Backfill uses the same `.set-or-append async` as normal execution. Re-running the same
  time range appends duplicate rows; use ADX extent management to deduplicate if needed.

### Status

Progress is tracked in `status.backfill`:

```yaml
status:
  backfill:
    requestId: jan-2026
    phase: Running             # Pending | Running | Completed | Failed
    observedGeneration: 3
    nextWindowStart: "2026-01-15T00:00:00Z"
    submittedWindows: 336
    completedWindows: 335
    retriedWindows: 2
    activeOperations:
      - operationId: "abc-123"
        startTime: "2026-01-15T00:00:00Z"
        endTime: "2026-01-15T00:59:59.9999999Z"
```

The current phase is also mirrored into `status.conditions` as `type: Backfill`:

- `True` when the backfill is complete
- `False` when the backfill fails
- `Unknown` while pending or running

### Completing or Cancelling

- **Completion**: The backfill transitions to `Completed` automatically when all windows are processed.
- **Cancel**: Remove the `backfill` block from the spec. The status will be cleared on the next reconcile
  (unless it was already in a terminal state, which is preserved for observability).
- **Restart**: Change the `requestId` to a new value with the desired time range.

> **See also:** [CRD Reference](../crds.md) for a summary of all CRDs and links to advanced usage.
