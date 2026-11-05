# Mock ingestor

A standalone, **destructive local load-test sink** for the collector's transfer
protocol. It stores nothing. A successful `202` tells the collector to **delete
its local WAL**, even though the mock discards all the data. Use disposable test
data only. There is no authentication or TLS; this is not a production ingestor.

```sh
go run ./tools/cmd/mock-ingestor \
  -listen-addr 127.0.0.1:9090 \
  -accept-rate 0.90 \
  -disconnect-rate 0.02 \
  -overload-scope table \
  -retry-after 5s \
  -response-delay 100ms \
  -seed 42
```

Point the collector at the **base URL**, not `/transfer`:

```toml
endpoint = 'http://127.0.0.1:9090'
```

For containers, bind the mock explicitly to a reachable interface, for example
`-listen-addr :9090`, and use its Docker-network hostname from the collector.
Container loopback refers to that container, not the host or another container.
Do not expose the unauthenticated mock to an untrusted network.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-listen-addr` | `127.0.0.1:9090` | Plain HTTP/1.1 address. Port `0` selects an ephemeral port, logged at startup. |
| `-accept-rate` | `1` | Per-attempt probability of selecting acceptance. |
| `-disconnect-rate` | `0` | Per-attempt probability of selecting an abrupt disconnect. |
| `-overload-scope` | `instance` | `instance` or `table`; applies to rejections. |
| `-retry-after` | `5s` | Positive duration, rounded **up** to integer seconds in `Retry-After`. |
| `-response-delay` | `0s` | Nonnegative delay **after** reading a full accepted/rejected body. Does not throttle upload reads. |
| `-seed` | Current Unix nanoseconds | Logged random seed. |
| `-stats-interval` | `10s` | Positive periodic summary interval. A final lifetime summary is always logged on shutdown. |
| `-shutdown-timeout` | `5s` | Positive graceful HTTP shutdown deadline; remaining server connections are forcibly closed afterward. |
| `-verbose` | `false` | Log each valid transfer's filename, request ID, selected outcome, and delay. Can become a load-test bottleneck. |

Rates must be finite and in `[0,1]`, with
`accept-rate + disconnect-rate <= 1`. The remaining probability selects
rejection. Invalid configuration exits nonzero before listening.

## Protocol and outcomes

Only `POST /transfer?filename=...` with a nonempty filename is a transfer.
Malformed methods/queries are rejected without consulting the policy. Filenames
are metadata only, never local filesystem destinations. Plain and gzip bodies
are opaque encoded bytes: the mock does not decompress or validate WAL data.
Each accept/reject upload is streamed with a fixed 32 KiB buffer.

- **Accept:** consume the entire body, wait the configured delay, return `202`.
  Interrupted/incomplete reads and canceled waits are never acknowledged with
  `202` or counted as accepted bytes.
- **Reject:** consume the entire body, wait the configured delay, return a
  complete `429` with `X-Adxmon-Overload-Scope` and `Retry-After`.
  Table overload also sets `X-Adxmon-Overload-Reason: mock-ingestor-policy`.
  Instance overload sets `Connection: close` for graceful connection churn;
  this is not an injected transport failure.
- **Disconnect:** hijack the HTTP/1.1 socket and immediately close it without
  consuming/closing the request body or writing an HTTP response. This produces
  a transport failure, without promising a particular TCP FIN/RST behavior.
  Hijacked sockets are explicitly tracked and closed on shutdown.

The collector currently marks an instance-overloaded or transport-failing peer
unhealthy for **one minute**. Table overload backs off the destination without
making the entire peer unhealthy, using `Retry-After` clamped by the client to
**300 seconds**. Acceptance probability is therefore **not** a proportional
throughput control. Retries are new attempts and may receive different outcomes.

The collector currently uses a **20-second response-header timeout** and a
**30-second overall transfer timeout**. Large response delays can exercise those
timeouts; the mock's delay is cancellation-aware, not a bandwidth limiter.
SIGINT/SIGTERM makes readiness fail, cancels reads/delays, closes hijacked
connections, shuts down the HTTP server, and prints final statistics.

Configuration is startup-only. For recovery, restart with full acceptance:

```sh
# Fail transfers without a peer-wide cooldown (stop with Ctrl-C to change phase).
go run ./tools/cmd/mock-ingestor -accept-rate 0 -overload-scope table -retry-after 1s
# Recover after stopping the failed phase.
go run ./tools/cmd/mock-ingestor
```

A seed reproduces the **decision sequence for a fixed decision order**, not
which concurrent request receives each outcome. Arrival order and retries change
that assignment. The internal concurrency-safe `policy` interface receives
filename/request-ID metadata and returns one outcome and a delay. Local tests
inject deterministic policies without coupling them to HTTP behavior.

## Readiness, metrics, and summaries

`GET /readyz` returns `200` while running (`503` during shutdown).
`GET /metrics` exposes an isolated Prometheus registry. Neither endpoint invokes
fault policy or contributes to transfer statistics.

Important series (all prefixed `mock_ingestor_`):

| Series | Meaning |
| --- | --- |
| `attempts_total{outcome="accept\|reject\|disconnect"}` | Valid attempts by **selected**, not completed, outcome. |
| `responses_total{status="202"}` / `{status="429"}` | Fully flushed accepted/overloaded responses; other fixed status values describe malformed requests or handler failures. |
| `disconnects_total` | Actual policy-generated hijacked socket closures. |
| `body_read_errors_total` / `response_write_errors_total` | Failed body reads / failed response flushes or unsupported hijacking. |
| `cancellations_total` / `client_cancellations_total` | Canceled transfer contexts including shutdown / canceled client request contexts. |
| `received_body_bytes_total` | Encoded bytes consumed, including partial uploads and rejected bodies; excludes HTTP framing. |
| `accepted_body_bytes_total` | Encoded bytes fully consumed and followed by a successfully flushed `202`. |
| `inflight` / `finished_total` | Active / finished transfer handlers, including malformed transfer requests. |
| `transfer_duration_seconds` | Histogram of full handler duration, including reads and delays. |
| `transfers_per_second` | Lifetime average finished transfer handlers/sec. |
| `received_body_bytes_per_second` / `accepted_body_bytes_per_second` | Lifetime encoded byte rates. |
| `uptime_seconds` | Time since statistics initialization. |

Outcome and status are the only metric labels; filenames and request IDs never
become labels. Default logging prints startup policy/seed and interval/final
summaries, not one line per request. Periodic rates describe the preceding
interval; final rates cover the process lifetime. Summaries also include the
interval's mean handler duration and lifetime maximum duration. Use Prometheus
`rate()` on counters for other windows.

Selected acceptance can exceed completed `202`s because uploads can fail or be
canceled. Even a successful server-side flush does **not** prove the collector
received its acknowledgment. Byte counts are not decompressed WAL bytes, unique
delivery counts, or deduplicated throughput: retries may be counted repeatedly.

## Validation

```sh
go test ./tools/cmd/mock-ingestor
go test -race ./tools/cmd/mock-ingestor
```

Tests exercise the real `cluster.Client` (including streaming gzip), typed
overload errors and retry metadata, early disconnects, deterministic policy
boundaries, bounded-size body reads, incomplete uploads, cancellation during
uploads and delay, shutdown/readiness/hijack tracking, concurrent metrics scrapes,
write failures, and goroutine leak detection. Collector WAL/backlog integration
and end-to-end file-generation smoke testing live outside this command.

Live configuration, scheduled phases, destination rules, capacity/bandwidth
models, partial-body/after-body faults, additional status outcomes, payload
inspection, authentication, and TLS are intentionally out of scope.
