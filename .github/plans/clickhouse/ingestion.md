# ClickHouse Ingestion Implementation Guide


## Target outcomes

- `ingestor/clickhouse` package implements the shared uploader interface and can be selected at runtime.
- ClickHouse inserts support CSV-decoded WAL batches. Native WAL ingestion is deferred until the MVP stabilizes.
- Retry/backpressure, health reporting, and metrics behave identically to the ADX backend.
- CLI configuration cleanly switches between ADX and ClickHouse modes with no double-writes.

## Component map

- `ingestor/adx`: Reference implementation for uploader lifecycle, metrics, and queue integration.
- `ingestor/service.go`: Entry point for wiring the uploader and CLI configuration.
- `cluster.Batch` + `UploadQueue`: Batch scheduling primitives reused by both backends.
- `pkg/wal`: Provides segment readers/writers and disk backpressure controls.

## Implementation phases

### Phase 1. Package scaffolding ✅

1. ✅ `ingestor/clickhouse` now exists with `config.go`, `schema.go`, and `uploader.go` scaffolding.
2. ✅ `Config` captures DSN, TLS, auth, queue sizing, and batch controls with validation + defaults.
3. ✅ `NewUploader(cfg Config, log *slog.Logger)` returns an implementation of the shared uploader contract and defaults to `pkg/logger` when caller passes `nil`.
4. ✅ Unit tests cover config validation, constructor defaults, lifecycle idempotence, and schema immutability.

Additional notes:
- `DefaultSchemas` mirrors `schema.NewMetricsSchema()` and `schema.NewLogsSchema()` so ClickHouse/ADX stay column-aligned.
- The uploader logs with the project-wide slog logger to match existing observability conventions.

### Phase 2. Connection and configuration plumbing ✅

- ✅ `buildOptions` constructs `clickhouse.Options` with TLS defaults, compression, async insert, and timeout knobs derived from `Config`.
- ✅ `clientPool` connection manager lazily dials the driver and is exercised by uploader lifecycle tests.
- ✅ Parse CLI-provided `<database>=<endpoint>` tuples via `kustoendpoints`, interpreting endpoints as ClickHouse DSNs.
- ✅ Extend `cmd/ingestor/main.go` to select `clickhouse.NewUploader` whenever `--storage-backend=clickhouse` (or equivalent) is set. Default remains ADX.

### Phase 3. Batch ingestion pipeline ✅

- ✅ `cluster.Batcher` and the shared `UploadQueue` feed the ClickHouse uploader exactly like the ADX path, so the storage backend remains pluggable.
- ✅ `Open` spins up `runtime.NumCPU()` worker goroutines that drain the queue and invoke `processBatch`, matching the ADX uploader’s concurrency/backpressure story.
- ✅ Each `processBatch` call opens WAL segments with `wal.NewSegmentReader`, reads the embedded CSV header, and derives a `schema.SchemaMapping` before preparing an insert writer through the connection manager.
- ✅ `ensureSchema` caches table definitions per schema signature, reusing column layouts across batches and keeping the ClickHouse DDL surfacing centralized.
- ✅ `convertRecord` converts CSV rows into typed ClickHouse values (including full-range `strconv.ParseUint` for unsigned columns) and relies on the batch writer to flush on row-count or interval triggers before finally sending the insert and removing WAL segments.

### Phase 4. Schema and metadata management

1. ✅ Introduce a `Syncer` analogue that ensures target tables exist before inserts:
   - ✅ For metrics, create schemas compatible with Prometheus samples (`timestamp`, `labels` map, `value`, etc.).
   - ✅ For logs, align with existing ClickStack/OTel schemas if available.
2. ✅ Run schema enforcement during `Open(ctx)` so the queue does not start until tables are ready.
3. ✅ Record schema version in a lightweight metadata table (optional but recommended for future migrations).

### Phase 5. Retry and backpressure integration

1. ✅ Propagate insert errors back to the shared queue. The queue will automatically requeue the batch at the tail, leveraging the WAL for durability.
2. ✅ Classify `clickhouse.ErrorCode` values into retryable vs. terminal buckets:
   - ✅ Retry transient network, timeout, and throttling codes.
   - ✅ Mark schema or validation failures as fatal and surface them through metrics/logging.
3. ✅ Do not implement an extra throttling layer; rely on the existing WAL guards (`--max-segment-count`, `--max-disk-usage`, `--max-segment-age`) to pause new intake until the queue drains.
4. ⏳ Extend shared backpressure gauges (queue depth, disk pressure) with a backend label so operators can distinguish ADX vs ClickHouse in dashboards. Dedicated ClickHouse counters/latency metrics live in `ingestor/clickhouse/metrics.go`; hooking the shared gauges remains outstanding.
5. ✅ Fatal ClickHouse errors now remove WAL batches before returning to prevent infinite retry loops.

### Phase 6. CLI wiring and feature flagging ✅

1. ✅ Add `--storage-backend` (values: `adx`, `clickhouse`) to `cmd/ingestor`. Default remains `adx` for backward compatibility.
2. ✅ Continue accepting `--metrics-kusto-endpoints` / `--logs-kusto-endpoints`; rename help text to “storage endpoints” and accept ClickHouse DSNs.
3. ✅ Validate endpoints during startup: ensure the database portion is non-empty and DSN parses successfully. Log a clear error if validation fails.
4. ✅ Disable ADX-specific flags/metrics when ClickHouse mode is selected to avoid confusion.

### Phase 7. Observability and health

1. ✅ Mirror `ingestor/adx/metrics.go` to emit counters/gauges for segments uploaded, bytes written, failures, and retry counts.
2. ✅ Add a health probe that performs a lightweight `SELECT 1` against the ClickHouse client during `Open` (periodic reuse can layer atop this probe).
3. ✅ Include structured logs for insert latency, batch sizes, and error details (with `clickhouse.ErrorCode`).
4. ✅ Disk audit scheduling now runs for both ADX and ClickHouse backends to maintain consistent backpressure monitoring.

### Phase 8. Testing and validation

1. ✅ Unit tests:
   - Mock the ClickHouse client interface to assert CSV handling and retry classification (`client_test.go`, `uploader_test.go`).
   - Ensure schema sync logic issues correct DDL statements (`syncer_test.go`).
2. ⏳ Integration smoke test (requires ClickHouse test container):
   - Start a ClickHouse instance.
   - Run the ingestor with `--storage-backend=clickhouse` and feed synthetic WAL segments (see `pkg/wal/testutil`).
   - Verify rows arrive in the target table and WAL segments are deleted.
3. ✅ Regression tests: run `go test ./ingestor/...` (CI + local) to confirm the ADX path still passes alongside ClickHouse.
