# ClickHouse Dev Stack Harness

This directory contains `dev_stack.sh`, a helper script that assembles a local
ClickHouse-backed pipeline so you can probe the collector → ingestor →
ClickHouse path without wiring a full Kubernetes environment.

## What the script does

- Builds fresh `collector` and `ingestor` container images from the current
  checkout (can be skipped with `SKIP_BUILD=1`).
- Creates a Docker network and persistent bind mounts under
  `tools/clickhouse/.stack/` for WAL and ClickHouse storage.
- Launches three containers:
  - `clickhouse` (via `clickhouse/clickhouse-server`)
  - `ingestor` configured for `--storage-backend=clickhouse`
  - `collector` configured to replicate to the ingestor with
    OTLP + Prometheus Remote Write endpoints enabled and a systemd journal log
    tap for `systemd-journald.service`
- Mounts the configs under `clickhouse-config/` so `http://localhost:8123/play`
  serves the Tabix SQL console and the default user profile matches the stack.
- Seeds ClickHouse with `observability` (metrics) and `observability_logs` (logs)
  databases plus a `default` user/password (default: `devpass`) and keeps label
  columns as plain strings so the stack works with stable ClickHouse builds
  without experimental JSON types.
- Templates a minimal collector TOML config that targets the chosen ClickHouse
  database and points at `https://ingestor:9090` using TLS skip verification.
  A static copy of this config is available at
  `tools/clickhouse/collector-minimal.toml` if you prefer editing it manually.

When the stack is up, you can interact with the components using the forwarded
ports on `localhost`:

| Component   | Purpose                         | Endpoint(s)                   |
| ----------- | ------------------------------- | ----------------------------- |
| ClickHouse  | SQL / native protocols + UI     | `http://localhost:8123` (Tabix UI at `/play`)<br>`clickhouse://localhost:9000`
| Ingestor    | Transfer / readiness / metrics  | `https://localhost:9090` (self-signed)<br>`http://localhost:9091/metrics`
| Collector   | HTTP API + OTLP receivers       | `http://localhost:8080`<br>`http://localhost:4318` (OTLP/HTTP)<br>`grpc://localhost:4317` (OTLP/gRPC)

## Requirements

- Docker 24.x or newer (the script uses `docker build`, `docker run`, and
  networks).
- Sufficient disk space for the generated `.stack` directory (hundreds of MBs
  once ClickHouse starts creating parts).
- Linux or macOS environment (Windows WSL works with Docker Desktop).

## Quick start

```bash
# Launch the stack (builds images unless SKIP_BUILD=1)
./tools/clickhouse/dev_stack.sh start

# Follow the ingestor logs
./tools/clickhouse/dev_stack.sh logs ingestor

# Tear everything down but keep data/config
./tools/clickhouse/dev_stack.sh stop

# Remove containers *and* the persisted data/config
./tools/clickhouse/dev_stack.sh cleanup
```

Environment overrides are documented in `dev_stack.sh`. Common flags:

- `SKIP_BUILD=1` — reuse previously built images.
- `CLICKHOUSE_IMAGE=clickhouse/clickhouse-server:24.9` — test a different
  ClickHouse release.
- `CLICKHOUSE_DB=my_metrics` — change the metrics database name (defaults to
  `observability`).
- `CLICKHOUSE_STACK_PASSWORD=supersecret` — override the dev ClickHouse
  password (user defaults to `CLICKHOUSE_STACK_USER`, default `default`).
- `CLICKHOUSE_STACK_USER=analyst` — change the ClickHouse username wired into
  the ingestor and README examples.
- `MOUNT_HOST_JOURNAL=1` — bind-mount the host's systemd journal into the
  collector container (also honor `COLLECTOR_JOURNAL_RUN_PATH`,
  `COLLECTOR_JOURNAL_VAR_PATH`, and `COLLECTOR_MACHINE_ID_PATH`).
- `LOGS_DB=mylogs` — set the database that receives journald-derived rows. The
  script respects ADX-style normalization, so avoid punctuation or spaces that
  ADX would strip.

## Seeding sample data

Once the stack is running you can push synthetic OTLP metrics through the
collector from another terminal. The HTTP receiver only accepts OTLP protobuf
payloads (`Content-Type: application/x-protobuf`), so the easiest way to send a
test signal is via the gRPC endpoint using [`grpcurl`](https://github.com/fullstorydev/grpcurl):

```bash
grpcurl -plaintext -d '{
  "resourceMetrics": [{
    "resource": {
      "attributes": [
        {"key": "service.name", "value": {"stringValue": "demo"}}
      ]
    },
    "scopeMetrics": [{
      "metrics": [{
        "name": "requests_total",
        "sum": {
          "aggregationTemporality": "AGGREGATION_TEMPORALITY_DELTA",
          "isMonotonic": true,
          "dataPoints": [{
            "asDouble": 1,
            "timeUnixNano": "'"$(date +%s%N)"'",
            "attributes": [
              {"key": "method", "value": {"stringValue": "GET"}}
            ]
          }]
        }
      }]
    }]
  }]
}' \
  localhost:4317 \
  opentelemetry.proto.collector.metrics.v1.MetricsService/Export
```

> **Tip:** If you prefer an HTTP client, tools such as
> [`otel-cli`](https://github.com/lightstep/otel-cli) can emit properly encoded
> OTLP/HTTP protobuf payloads. Plain `curl` with JSON will return `415
> Unsupported Media Type` and no samples will reach ClickHouse.

To inspect the ingested rows inside ClickHouse:

```bash
docker exec -it adxmon-clickhouse clickhouse-client \
  --query "SELECT name, value::Float64, labels FROM observability.metrics_samples LIMIT 10"
```

(Adjust the database/table if you launched the stack with a different
`CLICKHOUSE_DB` or custom schema mappings.)

## Viewing journal logs

The collector also tails `systemd-journald.service` via the journal reader. To
spot-check the ingested log rows, run:

```bash
docker exec -it adxmon-clickhouse clickhouse-client \
  --query "SELECT timestamp, level, message, journal_fields['PRIORITY'] AS priority FROM observability_logs.docker_journal ORDER BY timestamp DESC LIMIT 20"
```

The `journal_fields` map retains common metadata (PID, hostname, priority) so
you can filter or aggregate on them as needed.

Kubernetes discovery is disabled in this harness (`disable-kube-discovery = true`),
so the collector does not require a kubeconfig on the host.

## Browsing the ClickHouse UI

The harness exposes the Tabix console directly from ClickHouse. Once the stack
is running, open [http://localhost:8123/play](http://localhost:8123/play) in a
browser. Sign in with `default` / `devpass` (override via
`CLICKHOUSE_STACK_USER` / `CLICKHOUSE_STACK_PASSWORD`), then run SQL
queries against the local databases (for example `observability` and
`observability_logs`).

Try this aggregation in the editor to confirm metrics are flowing:

```sql
SELECT
  toStartOfMinute(timestamp)   AS minute,
  sum(value::Float64)          AS total_requests,
  groupArrayDistinct(labels['method']) AS methods
FROM observability.metrics_samples
WHERE name = 'requests_total'
GROUP BY minute
ORDER BY minute DESC
LIMIT 20;
```

If you have not pushed any OTLP data yet, the query will return an empty
result set—seed a metric first using the snippet in the section above.

Tabix assets are loaded from the official CDN, so the page requires outbound
internet access the first time it loads. If you prefer to pin a different UI,
edit `tools/clickhouse/clickhouse-config/play-ui.xml` and restart the stack.

## Cleanup cautions

- `stop` removes the containers but leaves `.stack/` intact so you can restart
  quickly.
- `cleanup` deletes the entire `.stack` directory. Back up any WAL or ClickHouse
  data you wish to keep before running it.
- The script intentionally uses deterministic container/image names. If you are
  running multiple stacks on the same host, override them before calling `start`
  to avoid collisions.
- If you seeded ClickHouse with an older schema (for example, when labels were
  stored in experimental JSON columns), run `./tools/clickhouse/dev_stack.sh cleanup`
  so the tables are recreated with the updated string-based layout.
