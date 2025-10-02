# Collector Storage Implementation Guide

_Last updated: 2025-10-01_

## Purpose

Provide an end-to-end recipe for preparing the collector’s WAL pipeline to serve both ADX and ClickHouse ingestion while keeping the CSV serialization for the first ClickHouse release.

## Target outcomes

- Collector exposes a backend selector (`adx` vs `clickhouse`) that stays in lockstep with the ingestor.
- WAL segments remain CSV/S2 encoded for the ClickHouse MVP, requiring no payload changes in the collector.
- Backpressure, replication, and durability semantics stay identical regardless of backend.
- Tests and rollout steps exist to confirm segments produced in ClickHouse mode are still valid CSV batches.

## Prerequisites

- Working knowledge of `storage.LocalStore`, `collector/service.go`, and the WAL helpers in `pkg/wal`.
- Familiarity with the new ingestor backend flag defined in the ClickHouse ingestion guide.
- Ability to run collector unit tests (`go test ./collector/...`) and WAL-focused tests (`go test ./storage/...`).

## Component map

- `cmd/collector/main.go`: CLI wiring and configuration surface for the collector.
- `collector/service.go`: Service assembly that constructs `storage.LocalStore` and WAL options.
- `storage/store.go`: WAL write path for metrics/logs (CSV encoders, S2 compression, sample metadata).
- `pkg/wal`: Segment framing, rotation, repository, and index used by both ADX and ClickHouse backends.

## Implementation phases

### Phase 1. Backend selector plumbing

1. Add `--storage-backend` (enum: `adx`, `clickhouse`) to `cmd/collector/main.go`, defaulting to `adx`. Mirror the flag via environment variable/config for parity with existing options.
2. Introduce a shared `storage.Backend` enum and propagate it through `collector.Config`, `collector.ServiceOpts`, and into `storage.StoreOpts` (collectors should import the storage type directly rather than re-wrapping it).
3. Emit an informational log on startup stating the active backend and refuse to start if the flag is empty or invalid. Keep ADX as the fallback when the flag is omitted.
4. Expose the selected backend via a gauge/metric (e.g., `collector_storage_backend_info`) so operators can validate clusters are homogenous before rollout.

### Phase 2. Keep WAL serialization CSV-first ✅ _Completed 2025-10-01_

1. Leave the existing CSV encoders (`transform.MetricsCSVWriter`, `transform.NativeLogsCSVWriter`, etc.) untouched; ensure no code path switches encoders based on the backend flag. **Status:** Verified — collector store continues to call the same CSV writers in ClickHouse mode.
2. Add a focused unit test under `storage/store_test.go` that writes data with the backend flag set to `clickhouse` and confirms the resulting segment can still be decoded by `wal.NewSegmentReader` as CSV. **Status:** Implemented via table-driven `TestStore_WriteTimeSeries`, which asserts identical CSV payloads for both `adx` and `clickhouse` backends.
3. Audit `storage.StoreOpts` to confirm S2 compression and `SampleType` metadata remain enabled; make compression configurable only if a future native payload path requires it. **Status:** Completed — storage options remain backend-agnostic, continuing to pass through compression metadata unchanged.
4. Validate that segment filenames and keys (`wal.Filename`, `storage.SegmentKey`) stay unchanged so ingestor batching continues to work without modification. **Status:** Confirmed — no key or filename changes were required, and tests continue to rely on the existing format.

### Phase 3. Safety and interoperability checks ✅ _Completed 2025-10-02_

1. Teach the collector to export the selected backend through its readiness/debug endpoints (e.g., include `backend=clickhouse` in `/readyz` or `/debug/store`) to aid rollout verification. **Status:** Completed — collector now serves `/readyz` and `/debug/store` with backend annotations.
2. Add a startup sanity check that compares the collector backend flag with an optional environment guard (e.g., `ADXMON_EXPECT_BACKEND`). This helps catch configuration drift during canaries. **Status:** Deferred — guard proved unnecessary and has been removed in favor of readiness endpoint validation.
3. Update documentation/config samples (collector README, charts, manifests) to surface the new flag and warn that all collectors and ingestors in an environment must share the same value. **Status:** Updated — documentation now focuses on `/readyz` and `/debug/store` for backend verification; no environment guard required.

### Phase 4. Validation & testing

1. Extend existing WAL integration tests (e.g., `storage/store_test.go`, `collector/service_test.go`) to run in both backend modes using table-driven cases. **Status:** Completed — storage WAL tests cover both backends for metrics and logs, and collector service tests now execute under both configurations.
2. Run `go test ./collector/... ./storage/...` locally and in CI to ensure no regressions. **Status:** Completed — local runs executed with both packages to confirm parity.
3. Perform an end-to-end smoke test: start a collector in `clickhouse` mode pointed at a local ingestor (still ADX-backed for now), generate sample Prometheus traffic, and confirm segments land on disk and can be uploaded by the CSV-based ingestor path. **Status:** Pending — requires a dedicated environment and manual verification.

## Rollout checklist ✅

- [ ] `--storage-backend` flag merged with default `adx` and documented.
- [ ] Collector exposes metrics/logs showing the active backend.
- [ ] Unit/integration tests cover both backend values without changing payload format.
- [ ] Operational runbooks updated with instructions for enabling ClickHouse mode.
- [ ] Canary collectors deployed alongside ADX ingestor to validate CSV compatibility before switching the ingestion backend.

## Follow-ups (post-MVP)

- Prototype native WAL payloads (`SampleTypeClickHouseNative`) once the ingestion side proves out, reusing the same backend flag.
- Revisit compression options for native payloads to avoid double compression.
- Formalize schema metadata persistence when moving beyond CSV serialization.
