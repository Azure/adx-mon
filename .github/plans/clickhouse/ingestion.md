# ClickHouse Ingestion Implementation Guide

Last updated: 2025-10-02

## Purpose

Equip the engineering team with a concrete, end-to-end recipe for adding the ClickHouse backend to the ingestor. Follow these steps to ship a production-ready uploader that slots into the existing WAL/queue pipeline without disrupting the ADX path.

## Target outcomes

- `ingestor/clickhouse` package implements the shared uploader interface and can be selected at runtime.
- ClickHouse inserts support both CSV-decoded WAL batches and pre-encoded native WAL segments.
- Retry/backpressure, health reporting, and metrics behave identically to the ADX backend.
- CLI configuration cleanly switches between ADX and ClickHouse modes with no double-writes.

## Prerequisites

- Familiarity with the current ingestor flow (`ingestor/service.go`, `cluster.Batcher`, `UploadQueue`).
- Understanding of WAL segment formats (`pkg/wal`, `SampleType*` enums) and the backpressure guards in `ingestor/storage`.
- Access to the ClickHouse server(s) and agreement on target databases/tables.
- Go modules for the ClickHouse driver (`github.com/ClickHouse/ch-go/v2`) vendored or pinned in `go.mod`.

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

### Phase 2. Connection and configuration plumbing

- ✅ `buildOptions` constructs `clickhouse.Options` with TLS defaults, compression, async insert, and timeout knobs derived from `Config`.
- ✅ `clientPool` connection manager lazily dials the driver and is exercised by uploader lifecycle tests.
- ⏳ Parse CLI-provided `<database>=<endpoint>` tuples via `kustoendpoints`, interpreting endpoints as ClickHouse DSNs.
- ⏳ Extend `cmd/ingestor/main.go` to select `clickhouse.NewUploader` whenever `--storage-backend=clickhouse` (or equivalent) is set. Default remains ADX.

### Phase 3. Batch ingestion pipeline

1. Reuse `cluster.Batcher` without modification; `UploadQueue` will continue to hand batches to the uploader.
2. Within `Upload(ctx, batch)`:
   - Acquire segment readers via `wal.NewSegmentReader`.
   - Inspect `segment.PayloadType`. If `SampleTypeClickHouseNative`, stream the raw bytes into a ClickHouse `proto.Block` without conversion. Otherwise decode CSV rows using existing CSV helpers.
   - Group rows by `<database>.<table>` prefix so each insert targets a single ClickHouse table.
3. Build columnar blocks with `proto.Block` helpers or the driver’s `AppendStruct` support. Apply LZ4 compression (driver default) for native mode.
4. Commit the batch using `client.Do(ctx, ch.Query{Body: block})` (or equivalent). On success, call `batch.Remove()` to delete WAL segments.

### Phase 4. Schema and metadata management

1. Introduce a `Syncer` analogue that ensures target tables exist before inserts:
   - For metrics, create schemas compatible with Prometheus samples (`timestamp`, `labels` map, `value`, etc.).
   - For logs, align with existing ClickStack/OTel schemas if available.
2. Run schema enforcement during `Open(ctx)` so the queue does not start until tables are ready.
3. Record schema version in a lightweight metadata table (optional but recommended for future migrations).

### Phase 5. Retry and backpressure integration

1. Propagate insert errors back to the shared queue. The queue will automatically requeue the batch at the tail, leveraging the WAL for durability.
2. Classify `clickhouse.ErrorCode` values into retryable vs. terminal buckets:
   - Retry transient network, timeout, and throttling codes.
   - Mark schema or validation failures as fatal and surface them through metrics/logging.
3. Do not implement an extra throttling layer; rely on the existing WAL guards (`--max-segment-count`, `--max-disk-usage`, `--max-segment-age`) to pause new intake until the queue drains.
4. Update backpressure metrics to include ClickHouse labels so operators can distinguish the backend in dashboards.

### Phase 6. CLI wiring and feature flagging

1. Add `--storage-backend` (values: `adx`, `clickhouse`) to `cmd/ingestor`. Default remains `adx` for backward compatibility.
2. Continue accepting `--metrics-kusto-endpoints` / `--logs-kusto-endpoints`; rename help text to “storage endpoints” and accept ClickHouse DSNs.
3. Validate endpoints during startup: ensure the database portion is non-empty and DSN parses successfully. Log a clear error if validation fails.
4. Disable ADX-specific flags/metrics when ClickHouse mode is selected to avoid confusion.

### Phase 7. Observability and health

1. Mirror `ingestor/adx/metrics.go` to emit counters/gauges for segments uploaded, bytes written, failures, and retry counts.
2. Add a health probe that performs a lightweight `SELECT 1` against the ClickHouse client during `Open` and on the existing health check interval.
3. Include structured logs for insert latency, batch sizes, and error details (with `clickhouse.ErrorCode`).

### Phase 8. Testing and validation

1. Unit tests:
   - Mock the ClickHouse client interface to assert CSV vs. native handling and retry classification.
   - Ensure schema sync logic issues correct DDL statements.
2. Integration smoke test (requires ClickHouse test container):
   - Start a ClickHouse instance.
   - Run the ingestor with `--storage-backend=clickhouse` and feed synthetic WAL segments (see `pkg/wal/testutil`).
   - Verify rows arrive in the target table and WAL segments are deleted.
3. Regression tests: run existing `go test ./ingestor/...` to confirm the ADX path still passes.

## Rollout checklist ✅

- [ ] Feature flag (`--storage-backend`) merged and defaulted to ADX.
- [ ] ClickHouse uploader passes unit and integration tests.
- [ ] Documentation updated (this guide, config reference, runbook).
- [ ] Dashboard/alert coverage for ClickHouse metrics in place.
- [ ] Run staged rollout: dev → staging → production. Monitor queue depth, disk usage, and error rates at each step.

## References

- `ingestor/adx` package for lifecycle and queue integration patterns.
- `pkg/wal` README for segment formats and backpressure controls.
- ClickHouse `ch-go` documentation: https://github.com/ClickHouse/ch-go
