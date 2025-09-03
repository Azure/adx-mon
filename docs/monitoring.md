# Monitoring: Log Loss Visibility

This guide describes how to configure Collector to emit per-table collected log counts for a curated allow‑list, and how to create a corresponding SummaryRule to count rows ingested into Kusto. This delivers *visibility* (not immediate root cause) into potential log loss.

> Stage Rationale: We prioritize low-risk observability first. If widespread loss is observed, we will justify targeted hot-path instrumentation (e.g. WAL metadata, sequence tracking) to localize loss sources.

## 1. Collector Configuration (TOML)
Add a `log_monitor` block listing `database:table` pairs to observe:

```toml
# collector-config.toml
[log_monitor]
# Monitored database:table pairs (case-sensitive as provided)
pairs = [
  "exampledb:ExampleLogs",
  "security:AuditEvents",
]
```

Behavior:
- Each listed pair increments the counter `adxmon_collector_monitored_logs_collected_total{database,table}` for every log that passes transforms and is sent to storage.
- Invalid entries (missing colon, empty db/table) are skipped with a warning.
- Restart resets counters (expected). Long-term comparison relies on Kusto-side counts.

## 2. Collector Metric
After deployment, scrape endpoint will expose for each monitored pair:
```
adxmon_collector_monitored_logs_collected_total{database="exampledb",table="ExampleLogs"} 123456
```
Interpretation: 123,456 log entries collected (post-transform) for that table since process start.

## 3. SummaryRule Example
Create a SummaryRule that (a) queries the source/log table and (b) writes an aggregated result into a *separate* destination table you specify via `spec.table`. The destination MUST NOT be the raw source table referenced in the KQL body.

Example: count rows from `ExampleLogs` into an hourly summary table `ExampleLogs_Counts` using a 1h interval and 1h ingestion delay (adjust both for your environment):

```yaml
apiVersion: adx-mon.azure.com/v1
kind: SummaryRule
metadata:
  name: logcount-exampledb-examplelogs       # CR name; not used inside query
spec:
  database: exampledb                       # Database containing BOTH the source log table and destination summary table
  table: ExampleLogs_Counts                 # Destination table that the controller writes output rows into
  interval: 1h                              # Execution cadence (also the window size)
  ingestionDelay: 1h                        # Delay to wait so data for the window is fully ingested
  body: |
    // Source: ExampleLogs (raw events)
    // Destination (spec.table): ExampleLogs_Counts
    // Controller injects _window_start (inclusive) and _window_end (exclusive).
    ExampleLogs
    | where ingestion_time() >= _window_start and ingestion_time() < _window_end
    | summarize RowCount = count()
    | extend WindowStart = _window_start, WindowEnd = _window_end
    // Make row idempotent: one row per window (optional uniqueness aids re-runs)
    | project WindowStart, WindowEnd, RowCount
```

Notes:
- `spec.table` is the OUTPUT table. Don't name it the same as the source table you read in the query body.
- The controller populates `_window_start` / `_window_end` based on `interval` & `ingestionDelay`.
- The first successful execution establishes the starting watermark. Backfill windows (if needed) can later be created by advancing `LastSuccessfulExecution` (controller managed) or via future backfill logic.
- If the summary table is empty initially, the first few windows might appear with 0 counts until the delay-elapsed windows are processed.
- A persistent negative delta (collector > Kusto) across multiple fully-settled windows signals likely loss; sporadic deficits that disappear later usually indicate late ingestion.

## 4. Comparing Counts
Conceptually (performed in dashboards / external analysis):
```
PotentialLoss ≈ CollectorCollected(window) - KustoIngested(window)
```
Where `CollectorCollected(window)` is the increase of the collector counter across the same window (requires counter delta & handling restarts), and `KustoIngested(window)` is the SummaryRule output.

Because counters reset on restart, prefer rate/delta over aligned windows and annotate restarts. For strict comparison you may store the collector counter deltas separately and join them to the summary output rows by matching on `[WindowStart, WindowEnd)`.

## 5. Operational Tips
- Start with a small critical set (5–20 tables).
- Revisit list monthly; prune obsolete tables to avoid metric cardinality growth.
- Ensure ingestion delay comfortably exceeds p95 end-to-end ingestion latency to minimize false deficits.
- If mismatches appear sporadically and shrink in subsequent windows, likely late ingestion, not loss.

## 6. Future Enhancements (Not Yet Implemented)
- Dynamic top-N monitored tables by recent volume.
- Automated SummaryRule generation & pruning (including destination table naming conventions).
- Internal WAL-based near real-time backlog metrics.
- Sequence range tracking for precise gap detection.
- Helper controller to auto-create `*_Counts` SummaryRules for a provided allow‑list.

