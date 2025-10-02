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
    OTLP + Prometheus Remote Write endpoints enabled
- Templates a minimal collector TOML config that targets the chosen ClickHouse
  database and points at `https://ingestor:9090` using TLS skip verification.

When the stack is up, you can interact with the components using the forwarded
ports on `localhost`:

| Component   | Purpose                         | Endpoint(s)                   |
| ----------- | ------------------------------- | ----------------------------- |
| ClickHouse  | SQL / native protocols          | `http://localhost:8123`<br>`clickhouse://localhost:9000`
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
- `CLICKHOUSE_DB=my_metrics` — change the target database name.

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
  --query "SELECT name, value::Float64, labels FROM observability.prometheus_samples LIMIT 10"
```

(Adjust the database/table if you launched the stack with a different
`CLICKHOUSE_DB` or custom schema mappings.)

## Cleanup cautions

- `stop` removes the containers but leaves `.stack/` intact so you can restart
  quickly.
- `cleanup` deletes the entire `.stack` directory. Back up any WAL or ClickHouse
  data you wish to keep before running it.
- The script intentionally uses deterministic container/image names. If you are
  running multiple stacks on the same host, override them before calling `start`
  to avoid collisions.
