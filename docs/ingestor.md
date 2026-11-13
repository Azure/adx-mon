# Ingestor Overview

The ingestor is an aggregation point for adx-mon to ingest metrics, logs and traces into Azure Data Explore (ADX)
in a performant and cost-efficient manner.

ADX recommends sending batches of data in 100MB to 1GB (uncompressed) [1]
for optimal ingestion and reduced costs.  In a typical Kubernetes cluster, there are many pods, each with
their own metrics, logs and traces.  The ingestor aggregates these data sources into batches and sends them to ADX
instead of each pod or node sending data individually.  This reduces the number of small files that ADX must later 
merge into larger files which can impact query latency and increase resource requirements.

# Design

The ingestor is designed to be deployed as a Kubernetes StatefulSet with multiple replicas.  It exposes several
ingress points for metrics, logs and traces collection.  The metrics ingress API is a Prometheus remote write endpoint and can support
other interfaces such as OpenTelemetry in the future.  

The ingestor can be dynamically scaled up or down based on the amount of data being ingested.  It has a configurable
amount of storage to buffer data before sending it to ADX.  It will store and coalesce data until it reaches a
maximum size or a maximum age.  Once either of these thresholds are reached, the data is sent to ADX.

Several design decisions were made to optimize availability and performance.  For example, if a pod is able to recieve data
it will store it locally and attempt to optimize it for upload to ADX later by transferring small segments to peers.
The performance and throughput of a single ingestor pod is limited by network bandwidth and disk throughput of attached 
storage.  The actual processing performed by the ingestor is fairly minimal and is mostly unmarshalling
the incoming data (Protobufs) and writing it to disk in an append only storage format.

# Data Flow

## Metrics

Each ingestor pod is fronted by a load balancer.  The ingestor receives data from the Kubernetes cluster via the 
Prometheus remote write endpoint.  When a pod receives data, it writes it locally to a file that
corresponds to a given table and schema.  These files are called Segments and are part of Write Ahead Log (WAL)
for each table. 

If Segment has reached the max age or max size, the ingestor will either upload the file directly to ADX or
transfer the file to a peer that is assigned to own that particular table.  The transfer is performed if the file
is less than 100MB so that the file can be merged with other files before being uploaded to ADX.  

If the transfer fails, the instance will upload the file directly. 

A segment of a table owned by another ingestor is normally transferred rather than uploaded from this node, so it
rotates after `--unowned-segment-max-age` (1 second by default) rather than `--max-segment-age`.  Its data reaches the
owner without waiting for the full segment age.  It is uploaded here instead when the owner is unavailable, when it is
older than `--max-transfer-age`, or when its batch reaches `--max-batch-segments` segments or the transfer size limit.
Ownership is checked when the table's WAL is created
and again every 10 seconds, so a table that moves between ingestors switches limits within one sweep.  Realtime tables
keep their own limits, and `--unowned-segment-max-age=0` turns this off.

Batches of a table that are waiting for a transfer worker are merged into one, up to `--max-batch-segments` segments
and `--max-transfer-size` bytes, so a slower owner receives fewer, larger transfers.  `--max-transfers-per-table`
limits how many transfers of one table run at once; later batches wait behind them and are merged.  Its default, 0,
sets no per-table limit, but at most 5 transfers run at once to each owner, one per connection to it, shared across
the tables it owns.  `--transfer-linger` sets how long a
closed segment of a table owned by another ingestor waits to be batched with others before it is transferred: 1 second
by default, or `--upload-linger` when set to 0.  If the owner answers that the table's segment is busy (423), the batch is retried
after 100–300 ms rather than after the usual retry backoff.

Uploads are merged the same way.  A closed segment of a table the ingestor owns waits `--upload-linger` (5 seconds
by default) to be batched with others.  Batches of a table that are waiting for an upload worker are merged into one
ingestion, up to `--max-upload-merge-segments` segments (500) and `--max-upload-merge-size` uncompressed bytes
(1 GiB), the files and size Kusto's default ingestion batching policy seals a batch at.  Each segment records its uncompressed size as it is written, appended to or opened
from disk; a segment without one is counted as ten times its size on disk.  `--max-uploads-per-table` limits how many uploads of one
table run at once; later batches wait behind them and are merged.  By default a table may use every upload worker
but the reserved ones, so batches only merge when Kusto is slow enough that they wait.  If a merged ingestion fails,
every segment in it is retried.

During upload, batches of files, per table, are compressed and uploaded to ADX as stream.  This allows many small
files to be merged into a single file which reduces the number of files that ADX must merge later.  Each batch is
sized to be between 100MB and 1GB (uncompressed) to align with Kusto ingestion best practices.

## Logs

## Traces

## Realtime ingestion

Queued ingestion is optimized for throughput and cost, so data typically takes minutes to become queryable.
Selected tables can instead use realtime ingestion, which uses Kusto
[streaming ingestion](https://learn.microsoft.com/en-us/azure/data-explorer/ingest-data-streaming) to make data
queryable within seconds.  Realtime tables still use the WAL on both the collector and the ingestor, so no data is lost
if a component restarts.

Realtime is a priority applied to the existing WAL, transfer and upload paths:

1. The collector rotates realtime WAL segments quickly (250ms by default), batches them as soon as they close, and
   transfers them to any ingestor ahead of queued tables.
2. The ingestor rotates and batches realtime segments the same way.  Realtime batches are never transferred to peers;
   the ingestor that receives them uploads them.
3. The ingestor ingests realtime batches with streaming ingestion, limited by a concurrency budget per Kusto endpoint.
4. If streaming ingestion is unavailable or a batch falls behind, the batch is ingested with queued ingestion
   instead.

Delivery is at least once.  A batch may be ingested twice if a streaming request succeeds but its response is lost
and the batch is retried or falls back to queued ingestion.

### Prerequisites

* [Enable streaming ingestion](https://learn.microsoft.com/en-us/azure/data-explorer/ingest-data-streaming#enable-streaming-ingestion-on-your-cluster)
  on the Kusto cluster.  Follower clusters must also enable it.
* The ingestor enables the streaming ingestion policy on each realtime table
  (`.alter table T policy streamingingestion enable`), which requires the Table Admin role.  If the ingestor lacks
  permission, enable the policy on the table or database yourself.
* Streaming requests are limited to 4MB of uncompressed data and share the cluster's capacity for concurrent
  streaming requests, which is roughly 6 per core.  Microsoft recommends queued ingestion for tables with more than
  about 4GB of data per hour.
* New tables and mappings can take up to 5 minutes to become available to streaming ingestion.  During that time,
  realtime batches use queued ingestion.

### Configuration

Configure the same tables on the collector, in the [`[realtime]`](config.md#realtime-ingestion) config section, and
on the ingestor.  If they differ, data is still ingested but may not be prioritized end to end.

| Ingestor flag | Default | Description |
| --- | --- | --- |
| `--realtime-table <db>.<table>` | | Table that uses realtime ingestion.  Can be repeated.  The database must be a configured metrics or logs database. |
| `--realtime-streaming-budget <endpoint>=<n>` | | Maximum concurrent streaming requests the ingestor deployment may send to a Kusto endpoint.  Required for each endpoint with realtime tables.  The budget is divided among ingestor peers so the deployment as a whole stays within it. |
| `--realtime-min-slots` | `1` | Minimum streaming slots per ingestor per endpoint.  If peers times this exceeds the budget, the budget is overcommitted. |
| `--realtime-max-slots` | `0` (no limit) | Maximum streaming slots per ingestor per endpoint. |
| `--realtime-max-segment-age` | `250ms` | Maximum age of a realtime WAL segment before it is rotated. |
| `--realtime-max-batch-latency` | `250ms` | Maximum time a closed realtime segment waits to be batched with others. |
| `--realtime-max-batch-bytes` | `512KiB` | Maximum size of a realtime batch in compressed WAL bytes.  Batches whose uncompressed data exceeds 4MB use queued ingestion. |
| `--realtime-max-lag` | `30s` | Realtime batches older than this use queued ingestion. |
| `--realtime-reserved-disk-bytes` | `1GiB` | Disk reserved for realtime segments when realtime tables are configured.  Other tables are limited to `--max-disk-usage` minus this value. |
| `--queued-reserved-workers-percent` | `10` | Percentage of transfer and upload workers reserved for queued batches so realtime traffic cannot starve them.  At least one worker is reserved. |

For example, with a 16 core cluster that supports about 96 concurrent streaming requests, leave headroom for other
clients and update policies:

```
--realtime-table Metrics.CpuUsage
--realtime-table Logs.ApplicationErrors
--realtime-streaming-budget https://mycluster.eastus.kusto.windows.net=75
```

With 5 ingestor replicas, each ingestor may send 15 concurrent streaming requests to the cluster.  If streaming
requests are throttled, an ingestor halves its limit and then raises it gradually as requests succeed.

If several ingestor deployments, such as deployments in different regions, send to the same Kusto cluster, divide the
cluster's capacity among their budgets since each deployment only knows about its own peers.

### Fallback to queued ingestion

| Condition | Behavior |
| --- | --- |
| Throttled or transient failure | Retried with streaming ingestion until the batch reaches `--realtime-max-lag`. |
| No streaming slot available | Retried after a short delay, without holding an upload worker, until the batch reaches `--realtime-max-lag`. |
| Batch older than `--realtime-max-lag` | Queued ingestion. |
| Uncompressed batch larger than 4MB | Queued ingestion. |
| Streaming unavailable for the table, such as when the policy is disabled, the schema has not propagated or the database is under maintenance | Queued ingestion, and the table uses queued ingestion for 5 minutes. |
| Other errors, such as invalid data | Queued ingestion. |

### Metrics

| Metric | Description |
| --- | --- |
| `adxmon_ingestor_realtime_batches_total{database,table,outcome}` | Realtime batches by outcome: `streamed`, `retry_<reason>` or `fallback_<reason>`. |
| `adxmon_ingestor_realtime_streaming_requests_total{database}` | Streaming requests sent. |
| `adxmon_ingestor_realtime_streaming_duration_seconds_total{database}` | Total duration of streaming requests.  Divide by the request count for the average duration. |
| `adxmon_ingestor_realtime_ingest_latency_seconds{database,table}` | Age of the oldest segment of the most recently streamed batch. |
| `adxmon_ingestor_realtime_streaming_slots{endpoint,state}` | Streaming slots by state: `budget`, `peers`, `share`, `limit` and `in_use`. |
| `adxmon_ingestor_realtime_streaming_overcommitted{endpoint}` | 1 when the minimum slots of all peers exceed the budget. |
| `adxmon_{ingestor,collector}_wal_segments_size_bytes_by_priority{priority}` | Size of closed WAL segments by priority: `realtime` or `queued`. |

A rising rate of `fallback_*` outcomes means realtime batches are using queued ingestion.  `fallback_lag` and
`retry_no_slot` usually mean the streaming budget is too small for the realtime volume, and `fallback_too_large`
means `--realtime-max-batch-bytes` should be lowered.

## Connection limit

`--max-connections` bounds the connections an ingestor holds, which bounds the memory a flood of connections can
take before their requests are admitted.  Each connection can hold up to 64 KB of request headers plus its TLS and
buffer state.  A connection beyond the limit is closed before anything is read from it, and counted by
`adxmon_ingestor_dropped_connections_total`; its sender sees the connection close and retries after its backoff.
With the default of 5000, 5000 connections each holding 60 KB of unfinished headers peaked at 1.7 GiB of heap on an
ingestor while thousands more were dropped.  Request bodies are bounded separately by `--max-intake-memory`, and a
body that does not fit is answered with 429.

Collectors keep up to 5 connections to each ingestor, including idle ones, so an ingestor receiving from N collectors
can hold up to 5 × N connections.

## Memory limit

`--memory-limit` sets Go's memory limit, a soft limit at which the garbage collector runs more often.  Without one,
the heap grows to about twice its live size before each collection, so under a flood of large transfers, each
leaving its buffer behind as garbage, an ingestor can hold several times what `--max-intake-memory` admits.  When
the flag is not set, the ingestor uses `GOMEMLIMIT` if it is set, and otherwise 90% of the container's memory limit
if the pod has one; the limit it uses is logged at startup.  The limit is soft: memory the ingestor really holds is
never refused, and the garbage collector uses at most about half the CPU to stay under it.

### WAL filename churn and kernel memory

WAL file creation and deletion can leave reclaimable filesystem metadata in the Linux kernel even after the
files are deleted. This memory can contribute to container working set without appearing in Go heap profiles.
Go's memory limit does not bound these kernel caches. Compare cgroup v2 `slab_reclaimable` and
`slab_unreclaimable` with `anon`, `file`, and post-GC Go heap before attributing rising working set to a Go leak.

When a segment rotates with no write pending, its successor is created lazily by the next write. This avoids an
unused empty successor after a burst, reducing unique filename churn and the associated demand for kernel
metadata. It also avoids holding that successor's file descriptor, flusher goroutine, and active writer state
while waiting for another write. Existing cached metadata is left for normal kernel reclamation, so deploying
this change does not imply an immediate drop in working set.

A local mixed-workload regression reduced unique creates/deletes by 10.6%, from 1,992 to 1,780, for the same
data output. **The reduction in Linux slab growth or container memory has not been measured.** The file-operation
percentage cannot be applied directly to memory usage: cache retention depends on filesystem activity and
reclamation, and continuously busy streams still create data-bearing segments. Pooled Go buffers can remain
retained, so reduced active segment state also does not establish a retained-heap saving. The first write after
an uncontended rotation now pays for file creation; validate latency on the actual WAL disk.

See the [WAL memory-impact investigation](designs/wal-filename-churn.md#expected-memory-impact) for measured
memory categories, limitations, and staging validation of sustained growth and reclamation under pressure.

## Latency metrics

Each stage records how long data waited before it left that stage, as histograms whose quantiles can be compared
against a latency target.

| Metric | Description |
| --- | --- |
| `adxmon_collector_transfer_latency_seconds{database}` | Age of the oldest collector segment in a batch when it is transferred to an ingestor. |
| `adxmon_ingestor_transfer_latency_seconds{database}` | Age of the oldest segment in a batch when it is transferred to the peer that owns the table. |
| `adxmon_ingestor_transfers_received_total{sender,code}` | `/transfer` requests received, by sender (`collector`, or `peer` for another ingestor) and response code.  A `423` means the table's segment was busy and the sender retries later. |
| `adxmon_ingestor_peer_transfers_total{outcome}` | Transfers sent to the ingestor that owns the table, by outcome: `accepted`, `locked`, `table_overloaded`, `peer_overloaded`, `too_large`, `exists`, `bad_request` or `error`. |
| `adxmon_ingestor_peer_transfer_segments` | Number of segments sent in each transfer to the ingestor that owns the table. |
| `adxmon_ingestor_upload_latency_seconds{database,method}` | Age of the oldest ingestor segment in a batch when it is uploaded, by `method`: `streaming` or `queued`. |
| `adxmon_ingestor_upload_duration_seconds{database,method}` | Duration of upload requests to Kusto. |
| `adxmon_ingestor_upload_failures_total{database,table}` | Queued uploads that failed and will be retried. |
| `adxmon_collector_scrape_duration_seconds{database}` | Duration of scraping a target, including writing its series. |
| `adxmon_collector_scrapes_skipped_total{reason}` | Target scrapes skipped because the previous scrape was still running (`in_flight`) or the collector was unhealthy (`unhealthy`). |

Segment ages restart at each hop, so these metrics show where time is spent rather than the end to end latency.  They
also exclude the time Kusto takes to make queued data queryable.  Measure end to end latency in Kusto:

```kusto
Cpu
| where ingestion_time() > ago(1h)
| summarize percentiles(ingestion_time() - Timestamp, 50, 99)
```

## ClickHouse sink

The ingestor can stream batches to ClickHouse in addition to Azure Data Explorer. Switch the storage
backend to `clickhouse` when you want to land telemetry in a ClickHouse cluster—either for hybrid
deployments or for local development.

### Configure the ingestor

1. Launch the binary with `--storage-backend=clickhouse` (or set
	`INGESTOR_STORAGE_BACKEND=clickhouse`).
2. Provide one or more metrics and logs endpoints with the existing
	`--metrics-kusto-endpoints` / `--logs-kusto-endpoints` flags. Each value uses the familiar
	`<database>=<dsn>` format. Example:

	```sh
	--metrics-kusto-endpoints "observability=clickhouse://default:devpass@clickhouse:9000/observability"
	--logs-kusto-endpoints "observability_logs=clickhouse://default:devpass@clickhouse:9000/observability_logs"
	```

	The ingestor automatically provisions the required tables (`metrics_samples` and `otel_logs`) and
	maps lifted labels/resources to ClickHouse columns.
3. TLS is disabled unless the DSN explicitly requests it. Use either an HTTPS-based DSN or append
	`secure=1`/`secure=true` when using the native protocol. Optional certificates can be supplied via
	the ClickHouse uploader configuration (CA, client cert/key, or `InsecureSkipVerify`).

> **Tip:** You can target multiple ClickHouse clusters by repeating the endpoint flags; the ingestor
> fans out batches to every configured DSN for a given stream.

### Align the collector

Collect the same WAL format by setting the collector configuration to `storage-backend = "clickhouse"`
(or pass the `--storage-backend` CLI flag). No other configuration changes are required—the collector
still delivers segments to the ingestor over the transfer API.

### Local harness

The helper script in `tools/clickhouse/dev_stack.sh` spins up a complete collector → ingestor →
ClickHouse pipeline on Docker. It builds fresh images (unless `SKIP_BUILD=1`), launches a ClickHouse
server with pre-created `observability` and `observability_logs` databases, and wires the collector to
the ingestor using the clickhouse backend. See [`tools/clickhouse/README.md`](https://github.com/Azure/adx-mon/blob/main/tools/clickhouse/README.md)
for usage, including how to seed OTLP metrics and query the data with `clickhouse-client` or the Tabix
UI at `http://localhost:8123/play`.

## WAL Format and Storage

The Ingestor uses a Write-Ahead Log (WAL) for durable, append-only buffering of telemetry data before upload to Azure Data Explorer. The WAL binary format is fully documented in [Concepts: WAL Segment File Format](concepts.md#wal-segment-file-format), including:
- Segment and block header layout
- Field encoding and versioning
- Compression (S2/Snappy)
- Repair and compatibility

For advanced troubleshooting, integrations, or recovery, see the [WAL format section](concepts.md#wal-segment-file-format) and the implementation in `pkg/wal/segment.go`.

[1] https://docs.microsoft.com/en-us/azure/data-explorer/ingest-best-practices
