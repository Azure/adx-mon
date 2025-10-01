# Feasibility of Adding ClickHouse Support to **adx-mon**

## Overview of ClickHouse Ingestion vs ADX (Kusto) Ingestion

**adx-mon’s current flow (for Kusto/ADX):** On-host collectors write metrics/logs into local WAL files. These are periodically rolled up into larger *segment files*. Central *ingestor* nodes then batch multiple segments into ~**1 GB CSV files** (optimal for Kusto) and upload them to blob storage for **queued ingestion** into Azure Data Explorer (Kusto). This decoupled approach provides reliability (using blob as durable queue) and leverages Kusto’s ingestion service.

**ClickHouse ingestion model:** ClickHouse is optimized for *batch inserts* directly into the database. It supports many input formats (CSV, JSON, Parquet, etc.), but critically the **Native** binary format (with compression) is fastest and most efficient. Unlike ADX, ClickHouse doesn’t require an external blob queue – data is typically inserted via SQL (`INSERT`) either through its native TCP protocol or HTTP API. ClickHouse *can* ingest from external files (e.g. via an S3/Blob storage engine or table function), but this usually requires a manual `INSERT ... SELECT` command to pull the file into a MergeTree table, rather than an automated queued service.

Another key difference: **optimal batch size**. ClickHouse’s MergeTree engine writes each insert as a new immutable *part*. Many tiny parts hurt performance (extra merge overhead). Thus, **batching is crucial** – inserts on the order of **10,000–100,000 rows (minimum ~1,000)** are recommended for efficiency. (In ADX, a few large files per batch was ideal; in ClickHouse, many medium-sized batches are better than numerous tiny ones.) By default, ClickHouse will internally accumulate up to ~1 million rows or ~256 MB per block before flushing to disk, so huge single inserts are not necessary – moderate batches suffice.

## Changes Needed in adx-mon for ClickHouse

To support ClickHouse, **adx-mon would need a new ingestion path** (a **ClickHouse-specific ingestor**), parallel to the existing Kusto ingestor:

* **Data formatting:** Instead of producing 1 GB CSV blobs, the ClickHouse ingestor should prepare data in a format optimal for ClickHouse. The simplest approach is to use the **ClickHouse native protocol** with compression (supported by official Go client libraries) to send batched records. This avoids CSV parsing overhead on the server and yields lower CPU and network use. Alternatively, adx-mon could still format CSV/JSON and use ClickHouse’s HTTP `INSERT` endpoint, but that is less efficient than the binary protocol.

* **Batch sizing:** The batching logic should be tuned for ClickHouse. Rather than waiting for ~1 GB segments, the ingestor might flush on reaching a certain number of events or bytes that aligns with ClickHouse’s sweet spot. For example, accumulating on the order of **tens of thousands of rows** per insert is a good practice. This ensures each insert forms a reasonably large part on disk without overwhelming memory. (By contrast, holding data until 1 GB may delay inserts too long or create extremely large inserts that the server will anyway split into parts.) We can configure thresholds (rows or bytes) for ClickHouse separate from Kusto’s.

* **Direct insertion vs staging to blob:** In a self-hosted ClickHouse scenario, the likely **best approach is direct ingestion** from the ingestor nodes to ClickHouse. The ingestor can maintain a persistent connection pool to the ClickHouse cluster and execute batched `INSERT` statements. This provides low latency and simplicity.

  Alternatively, one could mimic the ADX approach by **writing segment files to cloud storage and then instructing ClickHouse to ingest them** (e.g. using an S3/Azure Blob engine or table function to pull CSV/Parquet files). However, that adds complexity – an extra step to trigger ingestion on ClickHouse. Unless decoupling is needed for reliability, direct inserts are preferable. ClickHouse is designed to handle continuous micro-batches in real-time, so an external queue is typically unnecessary.

* **Schema and data mapping:** adx-mon will need to create or target the appropriate ClickHouse tables. Fortunately, ClickHouse’s **ClickStack** (observability stack) uses an OpenTelemetry collector that can automatically create tables like `otel_logs`, `otel_metrics`, etc., with suitable schemas. We should define analogous table schemas in ClickHouse for the data adx-mon collects (e.g. logs, metrics) – possibly mirroring the OTel schemas for compatibility. The adx-mon ClickHouse ingestor would then insert records into those tables. Data types and column mappings may need adjustment (e.g. ADX’s dynamic columns vs ClickHouse’s Map or JSON types).

* **Connection and error handling:** With direct inserts, adx-mon’s ingestor should handle ClickHouse availability issues. If the DB is down or slow, the ingestor might queue data in memory or on disk (similar to how the blob acted as a buffer for ADX). ClickHouse does offer **asynchronous insert** mode (buffering small inserts server-side), but in our case we can also simply retry on failures or batch a bit longer to wait for the cluster to recover. Using a library with built-in retry/backpressure will help.

## Data Format Expectations in ClickHouse

ClickHouse is very flexible on input format – it supports over 70 formats. For observability data, JSON or CSV could be used, but the **Native** binary format (with LZ4 compression) is **most efficient**. In practice, if we use an official Go client (such as [ClickHouse’s Go driver](https://github.com/ClickHouse/clickhouse-go)), it will handle using the native protocol under the hood (converting Go structs or column data into binary blocks). This yields minimal server-side parsing overhead.

If for some reason we choose a file-based pipeline, formats like **Parquet** or **CSV** could be considered. Parquet offers columnar compression, and ClickHouse can ingest it via file functions, but using Parquet would require adx-mon to generate that format and still orchestrate the load. CSV is simpler and what adx-mon already produces, but CSV ingestion in ClickHouse is slower (text parsing costs). Given **no limitation on third-party libraries**, leveraging the native client and its format is recommended for performance and simplicity.

## Optimal Segment (Batch) Sizes for ClickHouse

As noted, ClickHouse favors **micro-batches over huge files**. Each insert becomes a data part, and too many small parts cause excessive background merges. On the other hand, extremely large inserts use more memory and will anyway be broken into multiple parts if above certain thresholds (by default ~1e6 rows or 256 MB per part). Thus, an optimal strategy is to batch a reasonable number of records and insert frequently.

Key guidelines for batch size:

* **Aim for tens of thousands of rows per insert.** This falls in the recommended range (10k–100k rows) that yields efficient part sizes. If events are small (e.g. log lines), this might be on the order of a few MBs to tens of MBs per batch – a comfortable size for network transfer and memory.

* **Avoid tiny batches (<1k rows).** Very small inserts dramatically increase merge overhead. The ingestor should accumulate at least a minimum threshold (which could be configurable, e.g. 5,000 rows or a few MB) before flushing.

* **No need for 1 GB segments.** Unlike Kusto, ClickHouse does *not* need 1 GB files for optimal throughput. In fact, sending a full 1 GB CSV might parse slower than several smaller batches in native format. It’s more important to hit a good number of rows per insert than a specific byte size. In practice, a batch of 50k rows might be only e.g. 50 MB on wire (depending on data), but that’s fine – ClickHouse will merge parts in the background until parts reach ~150 GiB on disk through compaction.

* **Latency vs throughput trade-off:** Smaller, more frequent batches reduce latency (data is queryable sooner) but increase overhead. Larger batches maximize throughput but incur slight delays. We should choose a happy medium. For example, if adx-mon currently flushes every X seconds or on file rollovers, we might keep a similar time-based trigger but ensure we don’t flush too often with tiny chunks.

## Direct Ingestion vs Blob Staging

**Direct Ingestion (Recommended Path):** The new ClickHouse ingestor would connect directly to the ClickHouse service (over TCP or HTTP) and issue batched inserts. This is straightforward – essentially *replace* the `uploader.go` logic for Kusto (which writes to blob) with a **ClickHouse client** that writes into the appropriate table. The benefits are simplicity and speed: data goes straight into ClickHouse in real-time. Many organizations use ClickHouse this way for observability, often via an OpenTelemetry pipeline or custom collectors, to achieve high ingest rates with low latency.

**Blob Staging (Alternative):** This would mimic ADX’s queue: adx-mon could write segment files (CSV/Parquet) to cloud storage and then trigger ClickHouse to import them. Mechanisms to import include using the `S3` table engine or function (ClickHouse can read from AWS S3 or Azure Blob) in an `INSERT INTO ... SELECT` query. In a self-hosted setup, this means the ingestor would still need to **call ClickHouse** to perform the load after uploading the file. So, we haven’t eliminated the need to talk to ClickHouse; we’ve just inserted a file IO step in between.

*Pros:* Decoupling could add reliability – if ClickHouse is down, you could still dump files to storage and ingest later. It also offloads the heavy parsing to ClickHouse’s servers (which might parallelize reading the file).
*Cons:* More moving parts: you need to manage temporary files and cleanup, ensure ClickHouse knows about new files, and handle failures (what if the `INSERT...SELECT` fails?). Overall ingestion latency will be higher (must wait for file upload and then import). Given that ClickHouse is built for direct ingestion, this detour is usually unnecessary unless you specifically need a durable intermediate queue.

In summary, **ingesting directly into ClickHouse is the simpler and faster solution** for adx-mon. We can still maintain reliability by implementing retry logic or even writing to a fallback local disk buffer if the DB is unreachable, rather than building a full blob-ingestion pipeline.

## Leveraging OpenTelemetry (OTel) Support

It’s worth noting that ClickHouse’s observability stack (ClickStack) uses an **OpenTelemetry Collector** as the ingestion point. The OTel collector (with a *ClickHouse exporter*) listens on standard OTLP endpoints (gRPC 4317, HTTP 4318) and inserts data into ClickHouse. If adx-mon already uses OTel formats or collectors (it appears to support OTLP), an alternative approach could be to send data to an OTel Collector configured with ClickHouse exporter. In fact, the ClickHouse OTel exporter will automatically create the necessary tables and write data in bulk efficiently.

**Trade-off:** Using OTel Collector could reduce custom code – we’d reuse an existing pipeline (just point adx-mon’s output at the collector). However, it introduces another service/dependency (the collector process) and might limit some custom batching logic. Since adx-mon already has its own aggregation and batching, integrating directly via code (as described above) avoids an extra hop. That said, if adx-mon’s internal design is already modeled after an OTel Collector (with pluggable exporters), we could **repurpose the ClickHouse OTel exporter** within adx-mon. This would give us a proven ingestion mechanism with minimal reinvention. The decision comes down to architecture preference: standalone pipeline vs. OTel-based pipeline.

## Suggested Path Forward

Given the above, **my suggested approach is to implement a direct ClickHouse ingestion mode in adx-mon**, with the following reasoning:

* **High performance:** Use ClickHouse’s native protocol to send compressed batched inserts. This aligns with ClickHouse best practices (batching and binary format), ensuring we can ingest large volumes with minimal overhead.

* **Adjustable batching:** Introduce config for ClickHouse batch sizes (by row count and/or data size). We can tune this to achieve a balance between throughput and timeliness. Start with, say, 50k rows per insert (adjust as needed based on testing). This will prevent the “tiny part” problem without requiring 1 GB accumulations.

* **Maintain Kusto support via separate ingestors:** Architecturally, add a ClickHouse uploader/ingestor alongside the existing ADX one. The rest of adx-mon (collectors, WAL, etc.) can remain unchanged. At deployment, the user can choose the target (Kusto or ClickHouse), or even run both in parallel if needed. This keeps the codebase flexible and each backend optimized to its needs.

* **Use third-party libraries:** Take advantage of ClickHouse’s Go client or an OTel exporter library to handle the low-level details (connection pooling, data encoding). Since we have no restrictions on dependencies, this saves development effort and uses battle-tested implementations.

* **Consider fault tolerance:** Although we drop the blob-queue from ADX, we should implement robust error handling. For example, if an insert fails, the ingestor can retry, and if the ClickHouse cluster is down for extended time, perhaps buffer on disk. This ensures we don’t lose data in transient outages. (In practice, ClickHouse’s own async insert or Kafka ingestion could be explored later for even more decoupling, but initially simple retries should suffice.)

* **Leverage OTel if convenient:** If adx-mon can easily plug into the OTel pipeline, evaluate using the existing ClickHouse OTel collector/exporter. This might simplify schema management (as the exporter in ClickStack auto-creates tables with the recommended schema). The downside is adding an extra component. This decision can be made based on how adx-mon is currently structured.

By pursuing this plan, adx-mon can feed a self-hosted ClickHouse cluster with minimal friction. The end state would be a high-performance, on-prem observability pipeline: adx-mon collectors gather data, a ClickHouse-optimized ingestor batches and inserts those into ClickHouse (instead of Kusto). We would match ClickHouse’s preferred formats and batch sizes to achieve ingestion at scale, all while maintaining the existing Kusto support through a parallel path.

Overall, adding ClickHouse support appears **feasible and straightforward**: it mostly involves swapping out the final “upload” stage to use ClickHouse’s insert mechanism. Given ClickHouse’s flexibility and OTel integration, adx-mon can integrate without fundamental changes to its earlier stages. This opens the door to using ClickHouse as a backend for observability data, benefiting from its performance and the fact it even supports KQL for querying (so user queries and analytics can remain similar).
