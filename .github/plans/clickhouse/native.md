# ClickHouse Native WAL Payload Design

## Summary

Move the ClickHouse backend off of CSV payloads embedded in WAL segments and instead persist ClickHouse native binary blocks inside each segment. Collector nodes will encode metrics/log/logical batches straight into the native block format, write them to the WAL along with schema metadata, and the ingestor will detect the new payload encoding, hydrate the native block, and ship it directly to ClickHouse without going through CSV parsing. This document captures the end-to-end code changes required so that an implementation can proceed without additional design iterations. Wherever possible, the plan also flags hot paths so implementors can minimise allocations and CPU overhead while making the transition. **Assumption:** ClickHouse deployments never ingest CSV-formatted segments; clusters operate exclusively in native mode from the first boot.

## Format evaluation

### ClickHouse Native format (`Native`)

- **Reference:** [ClickHouse Native format](https://clickhouse.com/docs/interfaces/formats/Native)
- **Data layout:** Strictly columnar. Blocks are encoded column-by-column with metadata describing column types, making it a natural fit for `clickhouse-go/v2/lib/proto`’s `proto.Block` helpers. We batch rows in memory, populate per-column vectors, and hand the block to the driver which handles the binary framing and compression (LZ4 by default).
- **Implementation considerations:**
   - Requires holding one full block in memory before flushing, similar to today’s CSV batches, so we must right-size batch counts and aggressively reuse buffers via pooling.
   - True streaming (writing a row at a time) isn’t supported; we intentionally stick to batch-at-a-time semantics to avoid the complexity of manual columnar streaming.
   - Schema validation is automatic: ClickHouse rejects blocks whose column order/types don’t match the target table, giving us fast feedback in the ingestor path.
- **Performance outlook:** Eliminates CSV encode/decode loops entirely, keeps compression CPU inside the driver, and lets ClickHouse insert data without additional parsing.

### RowBinary format (`RowBinary`)

- **References:** [RowBinary format overview](https://clickhouse.com/docs/interfaces/formats/RowBinary) and [Importing RowBinary files via Native protocol](https://clickhouse.com/docs/integrations/data-formats/binary-native#importing-from-rowbinary-files)
- **Data layout:** Row-oriented; each row is written sequentially with binary-encoded fields. Easier to append incrementally because we can stream row-by-row without reorganising into columns.
- **Implementation considerations:**
   - Would still require custom encoders for every ClickHouse type we emit; the Go driver does not expose high-level RowBinary builders, so we’d own maintenance of that codec surface.
   - Table/schema mismatches would only surface after we send the payload, so validation is weaker than the native block helpers.
   - Because RowBinary lacks per-column compression, we’d rely on WAL-level (S2) compression only, which may increase payload size compared to native blocks that include LZ4 column compression.
- **Performance outlook:** Removes CSV parsing overhead, but keeps us on the hook for manual encoding work and forgoes the driver’s optimised columnar path. Attractive as a fallback if native blocks prove too heavy, yet not a clear win over the native plan when tooling effort is weighed in.

### CSV (status quo)

- **Reference:** [Importing CSV files](https://clickhouse.com/docs/integrations/data-formats/csv-tsv#importing-data-from-a-csv-file)
- **Characteristics:** Human-readable, simple to debug, and already implemented end-to-end in the collector and ingestor.
- **Limitations:**
   - Forces a double encode/decode loop (collector serialises to CSV, ingestor parses back into typed values) which dominates CPU for ClickHouse deployments.
   - Schema metadata is implicit in the header row, complicating validation and leaving us without structured schema hashes in WAL metadata.
   - Even if we shifted the ingestor to use ClickHouse’s bulk CSV import tooling, we’d still parse CSV on our side unless we rewired the pipeline to bypass the current batching flow entirely.
- **Use case:** Remains the default for ADX. For ClickHouse it’s best kept only as a last-resort fallback when native/RowBinary support is unavailable.

### Decision rationale

1. **Stick with ClickHouse Native blocks** for the primary implementation. It keeps the transformation layer aligned with ClickHouse’s canonical binary protocol, taps into the driver’s columnar encoding (and compression) without us writing bespoke codecs, and removes the hot-path CSV overhead we set out to eliminate.
2. **Document RowBinary as a contingency**. Should native block handling introduce unacceptable complexity (e.g., memory pressure from columnar buffers) we can pivot to RowBinary while still avoiding CSV parsing. The plan notes this fallback so we can change course quickly if benchmarks expose blockers.
3. **Restrict CSV to the ADX path and emergency fallback**. We call out its deficiencies explicitly so operators understand why ClickHouse clusters should not rely on it going forward.

This evaluation, combined with the WAL metadata work already in progress, underpins the implementation steps that follow.

## Goals

- Preserve the existing WAL durability guarantees while introducing a new binary payload encoding for ClickHouse segments.
- Allow collectors to emit ClickHouse-native WAL segments when `--storage-backend=clickhouse` is active, while keeping the ADX path untouched.
- Assume ClickHouse deployments only ever produce and consume native segments; no CSV/native mixed mode or migration path is required.
- Eliminate the CSV decode/re-encode loop inside `ingestor/clickhouse`, reducing CPU cost and improving throughput.
- Keep schema discovery and table creation logic aligned with the existing ClickHouse uploader.

## Non-goals

- Changing the ADX storage path or CSV segment format.
- Implementing ClickHouse native format ingestion for non-ClickHouse backends.
- Dropping S2 compression inside WAL blocks (we continue to compress native blocks before hitting disk).
- Replacing the current WAL file naming strategy or disk layout.

## Current state snapshot

- Collectors always serialize metrics/log batches as CSV lines via `transform/*.go` writers and call `wal.WAL.Write` with the raw CSV bytes.
- `pkg/wal` v1 blocks encode the CSV payload along with an 8-byte header (magic, version, sample type, sample count) and then S2-compress the block.
- ClickHouse ingestion re-opens the segment, reads the CSV header to recover the schema (`schema.UnmarshalSchema`), CSV-decodes every record, converts them into typed values, and uses `conn.PrepareInsert(...).Append`.
- Schemas are communicated implicitly through the first CSV line; there is no structured schema metadata in the WAL.

## Target architecture

```
Collector (ClickHouse backend)
  ↳ transform/native (metrics/log writers produce clickhouse-go proto.Blocks)
  ↳ wal.WAL.Write(payloadEncoding=native, schemaMeta)
  ↳ WAL on disk (S2-compressed binary blocks)
Ingestor (ClickHouse uploader)
  ↳ wal.Iterator detects payloadEncoding
  ↳ native block reader hydrates proto.Block + schema metadata
  ↳ connectionManager streams native block via clickhouse-go
ClickHouse cluster receives native blocks (no CSV parsing)
```

## Detailed plan

### 1. Extend WAL metadata to describe payload encoding

**Files:** `pkg/wal/segment.go`, `pkg/wal/iterator.go`, `pkg/wal/reader.go`, `pkg/wal/wal.go`, `pkg/wal/filename.go` (tests + helpers)

1. Introduce `type PayloadEncoding uint8` with constants:
   ```go
   const (
       PayloadEncodingUnknown PayloadEncoding = iota
       PayloadEncodingCSV
       PayloadEncodingClickHouseNative
   )
   ```
   Default to `PayloadEncodingCSV` for legacy ADX usage; ClickHouse mode always writes version 2 native blocks.
2. Bump `segmentVersion` to `2` and update the block layout produced in `segment.blockWrite`. New structure (before S2):
   ```text
   0-1   magic (0xAAAA)
   2     segmentVersion (2)
   3     sampleType (as today)
   4-7   sampleCount (uint32)
   8     payloadEncoding
   9     metadataVersion (start at 1)
   10-11 metadataLength (uint16)
   12..  metadata payload (metadataLength bytes)
   ...   raw payload bytes
   ```
   For CSV writes set `payloadEncoding=PayloadEncodingCSV`, `metadataVersion=0`, `metadataLength=0` to keep behaviour identical (value begins immediately at offset 12). For native writes, stash schema + auxiliary data inside `metadata` and set `payloadEncoding=PayloadEncodingClickHouseNative`, `metadataVersion=1`. **Performance note:** keep header construction on the hot path allocation-free by reusing the buffer pool already used in `blockWrite`; avoid transient slices when serialising metadata.
3. Update `WithSampleMetadata` to accept a header slice of at least 9 bytes and continue writing sample type / count at offsets `3` and `4:8`. Add helper `WithPayloadEncoding(PayloadEncoding)` and `WithPayloadMetadata([]byte)` that operate on offsets `8` and `10:12` respectively. Guard against overflow when `len(metadata)>math.MaxUint16`.
4. Adjust `segmentIterator.Next` to parse the new header:
   - For version ≥2, read the metadata section; copy it into iterator fields so callers can access both `PayloadEncoding` and the raw metadata bytes.
   - If a version <2 segment is encountered in ClickHouse mode, surface an error immediately (no automatic fallback to CSV readers).
   - Continue accumulating `sampleCount` into the iterator.
5. Extend the `Iterator` interface and implementations:
   - Add `PayloadEncoding() PayloadEncoding` and `PayloadMetadata() []byte` methods (ensure existing CSV callers still compile).
   - Update `SegmentReader.SampleMetadata` (or provide a new method) to surface the payload encoding for the current segment.
6. Update WAL tests (`pkg/wal/segment_test.go`, `wal_test.go`, `iterator_test.go`) to cover:
   - Writing version 2 blocks with metadata and reading them back.
   - Rejecting legacy version 1 segments when ClickHouse mode is active.
   - Error cases for truncated metadata or unsupported versions.

### 2. Schema metadata envelope for native segments

**Files:** `schema/schema.go`, new `schema/native.go` (if desired), `pkg/wal` tests.

1. Define a binary schema envelope structure used in WAL metadata:
   ```go
   type NativeSchemaEnvelope struct {
       SchemaVersion uint8 // always 1 for now
       SchemaID      uint64 // existing schema hash
       Mapping       schema.SchemaMapping // serialized JSON or binary form
       Table         string // normalized ClickHouse table name
       Database      string // normalized database
   }
   ```
2. Implement helpers `schema.MarshalNativeEnvelope(env NativeSchemaEnvelope) ([]byte, error)` and `schema.UnmarshalNativeEnvelope([]byte)` that use JSON for human-readability (size is small and it avoids inventing a custom binary codec).
3. When the collector writes native payloads, populate this envelope and pass it through `WithPayloadMetadata`.
4. Update WAL tests to assert that metadata survives round-trips and that `metadataLength` matches the encoded envelope. Include regression cases that assert no additional heap allocations are introduced when writing metadata (use `testing.AllocsPerRun`).

### 3. Collector-side native block writers

**Files:** `transform/` (new writers), `storage/store.go`, `collector/service.go`, `collector/metrics/...`, `collector/logs/...` tests.

1. Add new writers under `transform/`:
    - `transform/native_metrics_writer.go`: accepts `schema.SchemaMapping`, lifted columns, and `prompb.TimeSeries` batches. It should:
       - Resolve the ClickHouse column definitions using the same schema mapping currently produced for CSV headers.
       - Build a `proto.Block` (`github.com/ClickHouse/clickhouse-go/v2/lib/proto`) with typed columns (`proto.ColDateTime64`, `proto.ColUInt64`, `proto.ColJSON`, etc.).
       - Encode the block into an `io.Writer` via `proto.Block.Encode` (using LZ4 compression provided by the driver) and return the bytes.
       - **Performance notes:** cache column objects, reuse `proto.Block` instances (via pooling) instead of allocating per call, and pre-size column vectors based on sample counts to avoid repeated `append` growth. Ensure lifted label/resource strings reuse scratch buffers just like the CSV writers do today.
    - `transform/native_logs_writer.go`: analogous writer for OTLP/native logs, mapping to the ClickStack-compatible schema.
       - These writers should reuse existing lifting logic so that schema hashes remain stable between CSV and native paths. Pool the JSON/`[]byte` buffers used for OTLP bodies to keep GC pressure flat.
2. Introduce a `storage.PayloadFormat` enum (`CSV`, `ClickHouseNative`) and extend `StoreOpts` to include `PayloadFormat` (default `CSV`). For `BackendClickHouse`, force the format to native—no override is exposed when the backend is ClickHouse.
3. Modify `LocalStore.WriteTimeSeries` / `WriteOTLPLogs` / `WriteNativeLogs` to branch on the payload format:
   - For CSV format (ADX backend only) keep existing behaviour.
   - For native format (ClickHouse backend), use the corresponding native writer, collect the encoded bytes, and call `wal.Write` with `WithPayloadEncoding(PayloadEncodingClickHouseNative)`, `WithPayloadMetadata(schemaEnvelope)`, `WithSampleMetadata(...)` (sample counts still apply).
   - Ensure the returned byte slice is persisted as-is (no additional CSV header). The native writer should already include any driver-level header required for ClickHouse native format.
   - **Performance note:** when `ClickHouseNative` is active, avoid copying the encoded block before handing it to the WAL—write directly from the pooled buffer into the WAL block constructor.
4. Update CLI wiring (`cmd/collector/main.go`) and `collector.Config`/`collector.ServiceOpts` to automatically pin the payload format to native whenever `--storage-backend=clickhouse`; no user-facing flag is required to toggle on/off, and mixing formats is disallowed.
5. Emit startup logs showing both the backend and payload format. Add a Prometheus gauge label for the payload format (augment the TODO in `storage.md`).
6. Extend collector unit tests:
   - `storage/store_test.go`: new cases verifying that ClickHouse native segments contain version 2 headers with the expected metadata and that `wal.NewSegmentReader` reports `PayloadEncodingClickHouseNative`.
   - `collector/service_test.go`: ensure service starts with the new flag combinations and writes native segments when requested.

### 4. Ingestor: native block path

**Files:** `ingestor/clickhouse/uploader.go`, new helper files under `ingestor/clickhouse/`, `ingestor/service.go` (config plumbing), tests in `ingestor/clickhouse/*_test.go`.

1. Teach `processBatch` to obtain an iterator (`wal.NewSegmentReader`) and read the payload metadata once, failing fast if the encoding is not native:
   - If the iterator reports any encoding other than `PayloadEncodingClickHouseNative`, mark the batch as fatal and stop (no CSV fallback logic).
   - For native encoding:
     1. Parse the metadata envelope to recover database/table/schema mapping.
     2. Call `ensureSchema` with the mapping just like the CSV path (convert `schema.SchemaMapping` into `clickhouse` columns).
     3. Replace the CSV loop with a native block ingest pipeline:
        - Each iterator `Value()` now returns `[]byte` containing the ClickHouse native block as produced by the collector. Use `proto.Block.Decode` to hydrate into a `proto.Block` instance.
        - Validate that the block column signature matches the schema (names/order). If mismatched, treat as fatal.
        - Use a new connection manager method (e.g., `conn.InsertNative(ctx, database, table, block)`) that uses `clickhouse-go`’s `InsertBlock` or `NativeBatch` support (`conn.conn.ServerInfo()`, `conn.conn.InsertWithOption`, or low-level `conn.conn.AsyncInsert`).
        - Retain existing batching semantics by aggregating multiple WAL blocks before send when needed. Since native blocks already group rows efficiently, we can send each block individually or combine them via `block.Append`.
        - **Performance notes:**
          - Cache the decoded `proto.Block` and column buffers per worker; `proto.Block.Decode` can reuse destination slices when provided.
          - Avoid materialising intermediate `[]byte` copies—stream directly from the WAL reader into the decoder.
          - Track memory growth with `runtime/metrics` during development; regressions here will show up as extra GC cycles under production load.
     4. Ensure success removes the segment via `batch.Remove()` as before.
2. Implement support in `connectionManager` for streaming native blocks:
   - Add a method on `connectionManager` that returns a `nativeInserter` struct with methods `AppendBlock(proto.Block)` and `Send()` using `clickhouse.Client.InsertStream` with `clickhouse.Native` protocol. The driver exposes `ConnectNative` features via `client.Conn.Native().Block().Write`. Validate against library docs and adapt accordingly.
   - Provide retry logic identical to the CSV path; fatal schema errors should remove the segment. Ensure that retries do not rebuild the `proto.Block` from scratch—keep the decoded block in memory and replay it to ClickHouse to avoid duplicated decode costs.
3. Update uploader metrics (`metrics.go`) to increment counters for native vs CSV ingestion (label `payload_format`). Consider exporting allocation/cycle gauges from optional debug builds so we can observe perf deltas in staging. The ClickHouse implementation will always emit the native label at runtime; keep the label so shared dashboards remain uniform across backends.
4. Extend tests:
   - `uploader_test.go`: add cases that feed synthetic WAL files with native metadata and assert that the uploader calls the native insert path (mock connection manager to capture the block payloads).
   - Add fuzz/round-trip tests to confirm that a collector-produced native block (using the new transform writers) can be ingested and decoded back into ClickHouse rows matching the original samples.
   - Add benchmark-style tests (`go test -bench`) that compare allocations/latency between CSV and native paths to prevent regressions.

### 5. Configuration, observability, documentation

1. CLI flags:
   - `cmd/collector`: continue to expose `--storage-backend`; when `clickhouse` is selected the payload format is forced to native with no additional flag.
   - `cmd/ingestor`: reuse the existing backend selection flag; no payload-format override is provided because ClickHouse rejects non-native segments.
2. Metrics & logging:
   - Collector exposes `collector_storage_backend_info{backend="clickhouse", payload_format="native"}`.
   - Ingestor adds `ingestor_clickhouse_batches_total{payload_format}` (always `native` for ClickHouse) and logs the detected format per batch for debugging in case unexpected segments appear.
3. Documentation updates:
   - `.github/plans/clickhouse/storage.md` Phase 4 bullet can be updated once the metric lands.
   - `docs/ingestor.md`, `docs/config.md`: describe the new format flag and how the ingestor auto-detects payload encoding.
   - Add a migration note to `docs/cookbook.md` explaining how to switch clusters from CSV to native.

### 6. Operational expectations

ClickHouse clusters boot directly into native mode. There is no dual write, rollover, or migration process—operators must provision new installations with the desired backend and keep all collectors/ingestors aligned. Any CSV-encoded segments encountered while ClickHouse mode is active should be treated as configuration errors and rejected.

### 7. Testing matrix

- `go test ./pkg/wal ./storage ./transform ./ingestor/clickhouse` must pass.
- Add dedicated integration test under `tools/` or `ingestor/` that:
  1. Builds a WAL segment via the new native writers.
  2. Runs the uploader with a mock ClickHouse server (or the driver in local memory mode).
  3. Asserts successful ingest and WAL cleanup.
- Performance benchmark: extend `tools/bench` to compare CSV vs native ingestion throughput (optional but recommended). Capture allocation counts (`benchstat` on `-benchmem`) and CPU profiles for both paths so optimisations can be validated.

## Risks & mitigations

- **Driver encoding quirks:** clickhouse-go’s `proto.Block` API is low-level. Mitigate by encapsulating the encoding/decoding in dedicated helper packages with thorough unit tests and by validating against an actual ClickHouse instance in CI (can reuse the pending integration test).
- **WAL compatibility:** Bumping `segmentVersion` requires careful handling of mixed-version clusters. Even though ClickHouse mode does not support CSV, still fail loudly on unexpected versions so misconfigured nodes surface quickly.
- **Schema drift:** Any mismatch between collector-produced schema metadata and ingestion schema validation will produce fatal errors. Include schema ID/hash in metadata and log it prominently on both ends for debugging, and add unit tests that round-trip the mapping through the envelope.
- **Binary size growth:** Native blocks may be larger than CSV after S2 compression if not tuned. Monitor segment size metrics; we can adjust block row targets or disable redundant compression if needed (future optimization).
- **Pooling correctness:** Extensive reuse of buffers and blocks increases the risk of data races or accidental reuse after release. Guard pools behind tests that detect mutated data and run the race detector during CI (`go test -race`) for the new code paths.

## Follow-up work

- Investigate whether we should drop S2 compression for native blocks (they already contain LZ4-compressed columnar data) to save CPU.
- Explore writing shared WAL inspection tooling that prints payload encoding, schema, and row counts for debugging.
- Once native ingestion is stable, consider adding ClickHouse-native WAL support for OTLP traces when that backend is implemented.

