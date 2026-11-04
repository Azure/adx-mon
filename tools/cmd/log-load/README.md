# log-load

Generate application log load through either OTLP HTTP or a collector's
file-tailing pipeline. All examples run from the repository root.

## File output

```sh
go run ./tools/cmd/log-load \
  -target file:///tmp/opencode/log-load \
  -concurrency 3 \
  -batch-size 2500 \
  -total-samples 100000 \
  -database AKSinfra \
  -table IngestTest
```

The directory is created if needed. The files are opened **before generation**:

```text
/tmp/opencode/log-load/writer-0.log
/tmp/opencode/log-load/writer-1.log
/tmp/opencode/log-load/writer-2.log
```

Each writer exclusively owns one append-only file and buffer. Batches come from
a shared queue, so record counts are not necessarily balanced; a small run may
leave some files empty. Finite runs generate exactly `-total-samples`, including
a partial final batch, and wait for every writer to finish before reporting.
Existing writer destinations must be regular files; symlinks, FIFOs, directories,
and other special files are rejected without waiting for a FIFO reader.

Use `file:///absolute/directory`. URL-escape special path characters such as
spaces, `#`, `?`, and `%`. Hosts (including `localhost`), queries, fragments,
relative paths, and ambiguous forms such as `file://path/basedir` are rejected.
Only `http`, `https`, and `file` schemes are supported.

Each physical line is one application JSON object derived from the generated
OTLP record body, for example:

```json
{"kusto.database":"AKSinfra","kusto.table":"IngestTest","message":"something worth logging"}
```

This is **not** an OTLP envelope or protobuf JSON. Resource, scope, severity, and
original event timestamps are not preserved in file output. The collector
configuration below uses `plain` plus the `json` parser: its log timestamp is
when the line is read, not when it was generated. CRI output for timestamp
fidelity is not implemented.

Files are never truncated, rotated, or cleaned up. Prefer a **fresh output
directory** for each isolated test and a matching fresh collector storage/cursor
directory. With `-total-samples 0` (the default), generation continues until
SIGINT/SIGTERM or an error and can consume disk indefinitely.

Output is buffered and flushed at each batch boundary. Interruption stops queued
work, finishes the current file batch, and flushes/closes every opened file.
An open/write/flush/close failure fails the command and cancels other workers.
A flush exposes data to the tailer; it is **not** an `fsync` durability guarantee.
An I/O error can leave a partial final line; use a fresh directory after errors
instead of appending to that damaged line.

## Collector configuration fragment

```sh
go run ./tools/cmd/log-load \
  -target file:///tmp/opencode/log-load \
  -concurrency 3 \
  -database AKSinfra \
  -table IngestTest \
  -print-collector-config > /tmp/opencode/log-load-host-log.toml
```

This prints only TOML to stdout and exits without creating the target directory,
opening log files, or generating load. Incorporate the fragment into an existing
collector configuration; it is **not** a standalone configuration. It includes
one `[[host-log]]` block with Kubernetes discovery disabled and one exact
`[[host-log.file-target]]` per writer, with `log-type = 'plain'`,
`parsers = ['json']`, and the selected database/table. The collector does not
discover these files with directory scans or globs.

For a containerized collector, bind-mount the generator's directory and print
paths relative to its **absolute container-visible directory**:

```sh
go run ./tools/cmd/log-load \
  -target file:///tmp/opencode/log-load \
  -concurrency 3 \
  -collector-log-dir /load-logs \
  -print-collector-config
```

The printed paths are `/load-logs/writer-0.log` through
`/load-logs/writer-2.log`. The override affects only printed configuration; normal
generation still writes to `/tmp/opencode/log-load`. Keep concurrency identical
between configuration generation and the load run. The collector can start
before the files exist and will wait for them. For files without saved cursors,
it starts at the beginning; saved cursors resume previous reads.

## HTTP output

```sh
go run ./tools/cmd/log-load \
  -target http://localhost:4318 \
  -concurrency 3 \
  -total-samples 100000 \
  -database AKSinfra \
  -table IngestTest
```

The HTTP target is a **base URL**: `/v1/logs` is appended to its path. Requests
remain OTLP protobuf with `Content-Type: application/x-protobuf`. Database/table
options populate `kusto.database` and `kusto.table` in every record body; the
defaults remain `AKSinfra` and `IngestTest`.

Requests use a 30-second timeout and honor interruption. Transport errors,
non-2xx responses, and OTLP partial rejections fail the run rather than retrying
indefinitely or counting rejected records as successful output. Partial-rejection
errors include the rejected count and server message; accepted records in the
same response still count as written. Malformed, incomplete, oversized, or invalid
acknowledgments fail without counting records from that request. Config-printing
options require a file target.

## Dry runs and statistics

Add `-dry-run` to generate and count records without opening files, creating the
output directory, or making HTTP requests. It still validates all options. Use
a finite sample count or interrupt a continuous dry run.

The final report distinguishes generated records, written records/bytes, and
dry-run records, and reports elapsed time and written records/bytes per second.
`-verbose` adds progress on stderr once per second. Config-only stdout never
contains diagnostic or statistics text.

- **File:** written bytes are bytes accepted by the file (including newlines),
  and written records are complete newline-terminated records. Partial progress
  before an I/O failure is included. These measure **generator output**, not
  collector consumption or downstream delivery.
- **HTTP:** written records count records acknowledged by a valid 2xx OTLP
  response, minus any reported rejections. Written bytes count the full request's
  protobuf payload, including partially or fully rejected batches: these bytes
  cannot be attributed to individual accepted records. They exclude request
  headers and network overhead and do not prove downstream delivery.
- **Dry run:** generated/dry-run counts advance; written records and bytes remain
  zero.

On interruption or failure, generated counts can exceed written counts because
queued batches are discarded. Successful finite runs fully drain the queue.

## Tests

```sh
go test ./tools/cmd/log-load
go test -race ./tools/cmd/log-load
```
