# Table-scoped ingestion isolation

Collectors and ingestors apply local WAL backlog quotas to each normalized
`(database, table)` destination. All schema variants share a budget. Active
files, closed files, buffered writes, and in-flight reservations count toward
capacity. Usage is reconstructed from repaired WAL files on startup.

By default, a table may use 10% of the instance's `max-disk-usage` and
`max-segment-count`. Table quotas apply independently on every collector and
ingestor; these are not cluster-wide quotas. Global capacity limits remain the
last protection against aggregate exhaustion or a backend outage.

## Enforcement is opt-in

Table budgets start in **observe-only** mode: usage and would-be rejections are
measured and exported, but nothing is shed. This exists so limits can be sized
from real traffic before they affect delivery.

```sh
# collector
enforce-table-limits = true

# ingestor
ingestor --enforce-table-limits
```

Global `max-disk-usage` and `max-segment-count` are always enforced, including
in observe-only mode.

### Rollout

1. Deploy with the defaults and let traffic run.
2. Watch `adxmon_table_backlog_would_reject` and `adxmon_table_backlog_bytes`
   to find destinations that would be shed and how much headroom they need.
3. Add overrides for legitimately large tables.
4. Enable enforcement, then watch
   `adxmon_table_quota_rejected_records_total` and
   `adxmon_table_backlog_throttled_seconds`.

A table capped at 10% of one node is a real constraint: a destination's prefix
is owned by a single ingestor, so a genuinely large table needs an explicit
override rather than relying on the default. Sustained
`adxmon_table_backlog_throttled_seconds` is the signal that a budget is too
small rather than a neighbour being noisy.

## Collector configuration

```toml
max-disk-usage = 10737418240
max-segment-count = 10000
max-table-disk-usage = 1073741824
max-table-segment-count = 1000
enforce-table-limits = true

[[table-limits]]
database = "Logs"
table = "Application"
max-bytes = 2147483648
max-segments = 2000
```

Override names must match the final normalized database and table names. Omitted
override values inherit the default. Zero default limits select the automatic
10% budget. `disable-table-limits = true` disables default table quotas while
retaining explicit overrides and global protection.

## Ingestor configuration

```sh
ingestor \
  --max-table-disk-usage=1073741824 \
  --max-table-segment-count=1000 \
  --max-intake-memory=1073741824 \
  --enforce-table-limits \
  --table-limit='{"database":"Logs","table":"Application","max-bytes":2147483648,"max-segments":2000}'
```

Repeat `--table-limit` for additional overrides. `--max-intake-memory` bounds the
bytes buffered across concurrent transfer requests; disk budgets alone do not
protect process memory. Other required ingestor flags are omitted above.

## Shedding and recovery

When a table exceeds a quota, unrelated destinations continue being processed:

* Prometheus remote write returns success for mixed requests, intentionally
  discarding only over-budget samples.
* OTLP logs and metrics return partial success with rejected original record or
  data-point counts. Histogram and summary expansion does not multiply that
  count.
* Scraped metrics and native logs discard over-budget records and continue.
  Native-log cursors advance past intentional discards. Storage failures are
  still failures and are not acknowledged as successful store writes.
* Optional exporters do not decide acknowledgment. Only a durable sink does, so
  a failing exporter cannot make a producer resend records already stored.
* Internal segment transfers return `429`, `Retry-After`, and
  `X-Adxmon-Overload-Scope: table`. The sender retains its WAL files and backs
  off that table for that peer. Legacy or unrecognized `429` responses remain
  peer-scoped.
* A transfer larger than the destination's entire budget returns `413` with
  `X-Adxmon-Max-Transfer-Bytes`. The sender splits the batch and retries the
  halves. A single segment that still does not fit is retained, reported through
  `adxmon_ingestor_undeliverable_segments_total`, and backed off instead of
  retried forever; it needs a larger budget or a smaller `max-transfer-size`.

Table overload does not fail `/readyz` or mark a whole destination peer
unhealthy. Reaching a quota closes the table's active segment so the uploader
can drain it, which is what lets a throttled table recover even when its segment
has not reached its rotation size or age. Admission resumes below an 80%
low-water mark. Physical-file deletion releases capacity only after successful
removal, and a segment that fails to close is still indexed so its backlog
remains discoverable.

## Scheduling

Transfers and both ADX and ClickHouse uploads share workers with deficit round
robin, so a destination's share reflects bytes rather than batch count. A
destination sending batches far larger than the scheduling quantum is served
proportionally less often.

* Pending queues are bounded per destination; excess batches stay on disk and
  are rediscovered by the next scan. Deferrals are counted by
  `adxmon_ingestor_deferred_batches_total`.
* One worker is held in reserve so a newly active destination can start without
  waiting for an in-flight batch.
* Retry deadlines are tracked by the scheduler and do not occupy a worker.
* Idle destinations are retired, bounding both scheduling cost and metric
  cardinality.
* ADX schema operations are per-table and honour the upload context, so a slow
  mapping call for one table does not stall others.

## Visibility

Per-destination gauges carry `component`, `database`, and `table` labels:

| Metric | Meaning |
|---|---|
| `adxmon_table_backlog_bytes` / `_segments` | Current resident backlog |
| `adxmon_table_backlog_limit_bytes` / `_limit_segments` | Effective budget |
| `adxmon_table_backlog_throttled` | Currently over budget |
| `adxmon_table_backlog_throttled_seconds` | How long it has been over budget |
| `adxmon_table_backlog_enforced` | Whether enforcement is on |
| `adxmon_table_backlog_would_reject` | Observe-only rejections |
| `adxmon_table_backlog_oldest_age_seconds` | Age of the oldest resident file |

Cumulative counters: `adxmon_table_quota_rejected_records_total` and
`adxmon_table_quota_rejected_bytes_total` (`component`, `kind`, `reason`).
Rejected byte counts refer to uncompressed encoded input, while backlog bytes
are physical WAL bytes.

Scheduling state is exported as `adxmon_table_work_queued_batches`,
`running_batches`, and `retry_delay_seconds` by pipeline, database, and table.
Store debug output includes per-table usage, quotas, and rejection counts, and
quota-shedding logs are limited to one message per table per minute.

Per-table gauges disappear once a destination's backlog drains, bounding metric
label lifetime. Cumulative counters persist.

## Known limitations

* Quotas bound backlog, not rate. A destination that is already throttled is
  rejected before encoding, but sustained abuse is still bounded by budgets
  rather than by an explicit rate limit.
* Shedding policy is fixed: when a destination is over budget, new data is
  rejected. Preferring fresh data by dropping the oldest backlog is not
  implemented.
* Lossless retention of unbounded incoming data is impossible with finite
  storage. Mixed-request shedding intentionally prioritizes healthy-table
  availability.
