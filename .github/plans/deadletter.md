# Dead-letter capture for HTTP 400 during uploads

This document captures the problem, what the current code does, and a proposal to add a minimal-overhead “dead-letter” capture when Azure Data Explorer (Kusto) returns HTTP 400 for an upload.

## Problem statement

- We occasionally see HTTP 400 responses during segment uploads (observed by `pkg/http/logging_roundtripper.go`).
- We want to preserve a single example of the data that triggered a 400 for later inspection without letting the dead-letter area grow unbounded.
- Constraints:
  - Extremely hot path: uploading runs at high concurrency and throughput.
  - Do not fill disks or create churn. Only allow a single segment/file to occupy the dead-letter directory at any time.
  - Avoid coupling the capture logic to the HTTP transport (don’t buffer request bodies or re-read them in the RoundTripper).

Success criteria:
- On an HTTP 400 for an upload, atomically copy exactly one representative segment to `<storageDir>/deadletter/` if that directory is empty; otherwise, do nothing.
- Capture should add near-zero overhead in the success path and only minimal work on rare 400s.

## Current flow and observations

Where uploads happen:
- `ingestor/adx/uploader.go` is the worker that assembles a batch of WAL segments and uploads them via the Kusto SDK.
  - `upload()` gathers `[]*wal.SegmentReader` and builds a single `io.MultiReader(readers...)` to upload the entire batch as one payload.
  - It calls `uploadReader(mr, database, table, mapping)` which:
    - Ensures table/mapping via KQL mgmt calls.
    - Obtains/creates an `ingest.Ingestor` (queued/streaming or direct for Kustainer).
    - Calls `ingestor.FromReader(ctx, reader, ingest.IngestionMappingRef(...))` and then `res.Wait(ctx)`.

Error surfacing today:
- On `FromReader` error, `uploadReader()` calls `sanitizeErrorString(err)` which redacts SAS `sig=` parameters but converts the error to a plain `error` via `errors.New(errString)` — this loses the original type and metadata (including HTTP status codes from the SDK’s `*errors.HttpError`).
- On `res.Wait(ctx)` error, the error is a status record (SDK type) and not an HTTP response; it generally won’t include HTTP status.
- The HTTP-level logging (`pkg/http/logging_roundtripper.go`) logs `>=400` responses with safe correlation IDs, but it cannot attribute a specific segment because by then the request body is an aggregated `MultiReader`, and the RoundTripper doesn’t know the original files.

Implications:
- The best place to decide on dead-letter capture is in `uploader.upload()` immediately after `uploadReader(...)` returns an error, because at that scope we still know which batch/segments were being uploaded and can pick a representative segment path (we already compute `samplePath := segmentReaders[0].Path()`).
- We need to know whether the error was an HTTP 400. Because `sanitizeErrorString` erases types, we must preserve HTTP status information across the `uploadReader()` boundary.

## Proposal: lightweight dead-letter capture at uploader level

High-level design:
- Add a tiny typed error wrapper returned by `uploadReader()` when it detects an HTTP error (to preserve status code while keeping redaction):
  - `type UploadError struct { Err error; HTTPStatus int }` with `Error()` and `Unwrap()`.
  - Inside `uploadReader()`, when `FromReader(...)` returns an error, use `errors.As` to detect `*kustoerrors.HttpError` and record its `StatusCode`. Perform SAS redaction for logs but return the wrapped error so callers can branch on status.
  - For non-HTTP errors, return as-is (or wrapped with `HTTPStatus=0`).
- In `uploader.upload()`, after `uploadReader(...)` returns an error, check if it’s an `UploadError` with `HTTPStatus == 400`. If yes, attempt a single-file dead-letter capture using the first segment in the batch (`segmentReaders[0].Path()`) as the representative sample.

Dead-letter policy and mechanics (revised):
- Single fixed file in `storageDir`: create `<storageDir>/deadletter` (no extension) using `O_CREATE|O_EXCL|O_WRONLY`.
- Occupancy rule: The presence of this file itself is the guard. If the exclusive create fails with EEXIST, skip capture.
- Concurrency safety: `O_EXCL` provides a kernel-level gate across goroutines/processes; no directory creation/listing is needed.
- WAL interaction: The WAL repo only processes `.wal` files, so a file named exactly `deadletter` won’t be scanned or indexed.
- Copy approach: On first qualifying 400, copy the representative segment bytes into the newly created file and call `fd.Sync()`; leave the file present to block further captures until manually removed.
- Optional metadata: Log database, table, endpoint, and a sanitized error message alongside creation instead of writing a sidecar file (keeps the “one file only” policy simple).

Captured payload fidelity:
- The on-wire body is the concatenation of all batch segments via `io.MultiReader` with header skipping applied when a schema is present.
- To preserve exactly what Kusto saw, re-open each segment path in order with `wal.NewSegmentReader` and the same options (e.g., `wal.WithSkipHeader` when `schema != ""`), then stream them sequentially into the `deadletter` file. This reconstructs the original request body without buffering it during upload.

Minimal code changes (scoped and low-risk):
1. In `ingestor/adx/uploader.go`:
   - Define `type UploadError struct { Err error; HTTPStatus int }` and a helper `httpStatus(err error) int` that uses `errors.As` against `*github.com/Azure/azure-kusto-go/kusto/data/errors.HttpError`.
   - Update `uploadReader(...)` to:
     - Detect HTTP status (before redaction) when `FromReader(...)` returns an error and return `UploadError{Err: sanitizedErr, HTTPStatus: status}`. Keep current behavior for `res.Wait(ctx)` errors (usually not HTTP).
   - Add `func (n *uploader) tryDeadletterStream(segmentPaths []string, skipHeader bool, database, table string, cause error)` that:
     - Attempts `os.OpenFile(filepath.Join(n.storageDir, "deadletter"), O_CREATE|O_EXCL|O_WRONLY, 0600)`.
     - On success, iterates `segmentPaths` in order, re-opens each with `wal.NewSegmentReader` (applying `wal.WithSkipHeader` if `skipHeader`), and `io.CopyBuffer` each into the destination; then `Sync()` and close. On EEXIST, return immediately. Log a minimal, sanitized context line.
   - In `upload()`, when `err := n.uploadReader(...)` is non-nil, check `errors.As(err, &UploadError)` and `ue.HTTPStatus == 400`. If so, derive `segmentPaths` from `segmentReaders[i].Path()` and `skipHeader=(schema!="")`, then call `n.tryDeadletterStream(segmentPaths, skipHeader, database, table, err)` before returning.
2. No changes to `pkg/http/logging_roundtripper.go` are required. It continues to log errors with redacted SAS and correlation IDs.

Performance considerations:
- Success path remains unchanged: no additional allocations or syscalls.
- Failure path on non-HTTP or non-400 errors: unchanged aside from the cheap `errors.As` check.
- Failure path on HTTP 400: single O_EXCL file create and a sequential copy of the batch’s segments into the deadletter file, performed only once until the file is removed. No impact on success path.
- No buffering or duplication of request bodies in memory.

Edge cases and behaviors:
- Partial write on crash: The deadletter file may be incomplete but still fulfills the goal of blocking further captures and providing a sample. Operators can inspect/remove it.
- Shared storage: If multiple instances share `storageDir`, the O_EXCL gate ensures only one deadletter capture across instances, which is desirable for "one sample at a time".
- Direct ingest (Kustainer): `testutils.NewUploadReader` performs inline `.ingest inline` via Mgmt. HTTP 400s still surface via SDK error types; classification works the same.
- Schema/mapping failures before reading data: These can also return HTTP 400 (e.g., invalid mapping). We still capture the representative segment to help debug mismatches.
- If the `deadletter` file already exists, further captures are skipped until an operator removes it — matching the “only one at a time” requirement.
- If the error string contains SAS tokens, our existing redaction will be applied in logs and the `.meta.json` content should also be sanitized using the same regex.

Open questions / options:
- File naming: Fixed name `deadletter` keeps things simplest and avoids directory listing; operators manually remove it after investigation.
- Config flag: Optional; defaulting to enabled is reasonable given strict single-file cap and low overhead.

## Test plan (follow-up)
- Unit tests for:
  - `httpStatus(err)` classification with a crafted `*kustoerrors.HttpError` (status 400 vs other).
  - `tryDeadletter(...)` concurrency: multiple goroutines attempt capture; only one file is created.
  - Occupancy rule: if directory is pre-populated, capture is skipped.
- An integration-style test that simulates a 400 from `ingest.FromReader()` and asserts one file appears in the dead-letter directory.

## Summary
This approach adds a tiny, well-contained hook at the uploader layer to detect HTTP 400s (without changing the transport) and captures the exact batch payload (reconstructed stream of all segments) to `<storageDir>/deadletter`. It preserves hot-path performance, avoids unbounded disk growth via a single fixed file, and provides a faithful artifact for debugging.
