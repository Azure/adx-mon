# Log Loss Visibility (Phase 0)

## Objective
Introduce low-impact visibility into potential log loss by reconciling Collector-side counts of selected (database, table) pairs with authoritative row counts in Kusto, using existing SummaryRule infrastructure. Avoid any modifications to hot WAL / upload paths initially.

## Non-Goals (Phase 0)
* Real-time (sub-minute) loss detection.
* Attribution of loss stage (collector vs ingest vs Kusto).
* Comprehensive coverage of every table (cost / scale concerns).
* WAL metadata changes or per-block sequence tracking.

## High-Level Approach
1. Add a Collector configuration surface in the existing TOML (`cmd/collector/config`) to declare an allow-list of monitored DB/Table pairs (no new CLI flags).
2. Collector exports a new counter: `adxmon_collector_monitored_logs_collected_total{database,table}` incremented only for monitored pairs after transforms succeed and immediately before logs are written to storage.
3. Provide documentation (Monitoring guide) instructing operators how to create SummaryRule CRs (manually or via automation) referencing the same (database, table) pairs with an ingestion delay (e.g. 1h) to count rows ingested in Kusto over fixed windows.
4. External dashboards / future rules compare long-term divergence; immediate alerting deferred until confidence built.

## Rationale
- Minimal risk to hot path: counting is a simple in-memory aggregation per batch for a bounded set of pairs.
- Kusto is the source of truth for delivered data; collector-only counters alone cannot confirm delivery success.
- Delay window (e.g., 1h ingestion delay) reduces false positives due to late ingestion.
- Allow-list constrains operational & cost overhead; evolves later (top-N, auto-selection) if needed.

## Configuration Design
TOML block (new) under root config:
```toml
[log_monitor]
# List of database:table pairs to monitor for loss visibility.
pairs = [
  "prodlogs:ContainerLogs",
  "prodlogs:AuditEvents",
]
# (Optional future) ingestion_delay = "1h"  # Default documented; not parsed for Phase 0.
```
Parsing Rules:
* Each entry format: `<database>:<table>` (both required; case-sensitive as provided; normalization left to existing pipeline).
* Invalid entries logged & skipped (no startup failure unless list exclusively invalid).
* Stored internally as `map[string]struct{}` keyed by `db + "\x00" + table` (zero byte separator avoids accidental collisions).

Config Struct Additions:
```go
type LogMonitor struct {
  Pairs  []string `toml:"pairs"`
  parsed map[string]struct{} `toml:"-"` // internal set of monitorKey(db,table)
}

type Config struct { /* existing fields */ LogMonitor *LogMonitor `toml:"log_monitor"` }

func monitorKey(db, table string) string { return db + "\x00" + table }
```
Validation / Parsing Behavior:
* Empty or nil `log_monitor` block => monitoring disabled (no error).
* Each entry must contain exactly one colon and non-empty db & table segments.
* Invalid entries: warn + skip. If all invalid, proceed (effectively disabled).
* Soft cap: if valid pair count > 500 (`MaxMonitoredPairsWarn`), emit warning; continue.
* Pointer retained for potential future differentiation of unset vs empty.

Future Extensions (not now):
- Dynamic top-N selection mechanism.
- Regex support.
- Per-pair override of delay.

## Metric Specification
Name: `adxmon_collector_monitored_logs_collected_total`
Symbol: `metrics.MonitoredLogsCollectedTotal` (CounterVec)
Labels: `database`, `table`
Semantics: Number of log entries that passed transforms and were accepted for write (attempted persistence) for monitored pairs.
Exclusions: Logs with missing / invalid destination attributes, dropped earlier, or not in allow-list.
Reset Behavior: Process restart resets counter (documented; long-term reconciliation tolerates it by using rate / windowed comparisons later if needed).
Naming Rationale: Prefix `monitored_` to scope the subset; suffix `_collected_total` mirrors existing pattern (`logs_received_total`, etc.).

## Increment Site
Location: `collector/logs/sinks/store.go` inside `Send` BEFORE invoking `store.WriteNativeLogs` (single traversal). Slight overcount possible if write ultimately fails; accepted trade-off.

Chosen Strategy:
* Pre-write counting avoids a second pass or per-log metric increments.
* Aggregation groups logs by (db,table) only if pair monitored; single `Add()` per pair per batch.
* Lazy map allocation: allocate counts map only on first monitored hit to keep overhead negligible for unmonitored batches.
* No mutation of logs; standard `batch.Ack()` semantics preserved.

Pseudo-code:
```
var counts map[string]int
for _, log := range batch.Logs {
  db := attr(dbAttr); tbl := attr(tblAttr)
  if !isMonitored(db,tbl) { continue }
  if counts == nil { counts = make(map[string]int) }
  counts[monitorKey(db,tbl)]++
}
for k,c := range counts { db, tbl := splitKey(k); metrics.MonitoredLogsCollectedTotal.WithLabelValues(db, tbl).Add(float64(c)) }
// then call store.WriteNativeLogs(ctx, batch)
```
Concurrency: Each sink invocation handles its own batch; Prometheus client counters are concurrency-safe. Monitored set is read-only after construction.

Thread Safety / Performance:
* Per-batch temporary map only; no global locks.
* Monitored set bounds metric series cardinality; restarts reuse same series; no churn.
* Zero-byte key separator avoids collisions (e.g. `a\x00bc` distinct from concatenations without separator).

## Documentation Instead of Embedded Template
Rather than shipping a baked templating mechanism now, we will document a contrived example in a new Monitoring guide demonstrating:
1. Collector TOML snippet with a monitored pair (e.g. `exampledb:ExampleLogs`).
2. A matching SummaryRule YAML manifest counting rows over a 1h window with a 1h ingestion delay.
3. How to interpret the collector counter vs the SummaryRule output.

The SummaryRule spec body is manually defined by the operator; automation can be considered later if pairing becomes operationally heavy.

## Failure / Edge Handling
- If table does not exist yet: Kusto returns zero rows; treat as 0. (Optional later: skip until first non-zero to reduce noise.)
- Allow-list entry removed: SummaryRule left orphaned unless operator prunes; add future cleanup task.
- High churn tables: Operator/user responsibility to curate list.

## Alternatives Considered (Rejected For Phase 0)
1. WAL sample metadata counting: richer, faster, more intrusive.
2. Sequence numbering for exact gap detection: complexity and header format changes.
3. Parsing segments at ingestor for counts: added IO & CPU in hot path.

## Open Questions
* Second metric for rejected (monitored) logs missing destination? (Deferred.)
* Gauge for current monitored set size? (Deferred.)
* Operator automation for SummaryRule creation? (Phase 1+.)
* Write error counter to estimate potential overcount delta? (Deferred.)

## Implementation Steps (Phase 0)
1. Add metric definition in `metrics/metrics.go` (symbol `MonitoredLogsCollectedTotal`).
2. Extend `Config` with `LogMonitor` + helper `monitorKey` + lazy parsed set + validation rules & warnings.
3. Build monitored set once in `main.go` after `Validate()`.
4. Pass monitored set into each `StoreSink` (extend `StoreSinkConfig`).
5. Implement counting logic in `StoreSink.Send` (aggregate per batch before write).
6. Documentation already added at `docs/monitoring.md`; add to `mkdocs.yml` nav + link from `docs/index.md`.
7. Unit tests:
  - Config: valid, invalid, empty, >500 warning path.
  - StoreSink counting: monitored vs unmonitored, multi-pair aggregation, empty set no increments.
8. Run build & tests.

## Risks & Mitigations
| Risk | Mitigation |
|------|------------|
| Performance regression due to per-log map lookups | Limited to monitored pairs; early continue for non-monitored; typical monitored set small. |
| Overcount if WriteNativeLogs fails | Low failure rate; could move increment after success later if needed. |
| Restart resets counters, confusing long-window comparisons | Document; rely on windowed SummaryRule counts for authoritative ingestion numbers. |
| Configuration drift across collectors (partial coverage) | User ensures homogenous deployment or tolerates partial counts; future central config may enforce. |

## Future (Phase 1+) Preview
- Dynamic top-N monitoring (periodic selection based on recent volume metric already emitted or new internal counters).
- Add ingestion success vs collected delta dashboard.
- Introduce internal WAL-based backlog metric for faster detection if justified.

---
## Current Rationale / Stage Note
We are intentionally in a "visibility first" stage. If data reveals widespread or systematic log loss, that evidence will justify carefully introducing additional (more invasive) hot-path instrumentation (e.g., WAL metadata, sequence tracking) to pinpoint *where* loss occurs. Prematurely modifying performance-critical paths without confirmed need carries unnecessary risk.

Feedback welcome before implementation.
