# Guides

## End-to-end ClickHouse pipeline

This guide walks through wiring the collector, ingestor, and local tooling to land telemetry in
ClickHouse instead of Azure Data Explorer. Follow it when you want to evaluate the ClickHouse backend
outside of Kubernetes or run hybrid deployments.

### Prerequisites

- A reachable ClickHouse cluster (self-hosted, managed service, or the dev harness below).
- Credentials with privileges to create databases/tables or an existing schema that matches the
	default metrics/logs layout (`metrics_samples`, `otel_logs`).
- `collector` and `ingestor` binaries or container images built from this repository.

### 1. Configure the ingestor

Select the ClickHouse backend and provide DSNs per stream using the existing endpoint flags. Each
value follows the `<database>=<dsn>` convention:

```sh
ingestor \
	--storage-backend=clickhouse \
	--metrics-kusto-endpoints "observability=clickhouse://default:devpass@clickhouse.internal:9000/observability" \
	--logs-kusto-endpoints "observability_logs=clickhouse://default:devpass@clickhouse.internal:9000/observability_logs"
```

Key details:

- Supported DSNs include `clickhouse://`, `tcp://`, and `https://`. Use `secure=1` (or `secure=true`)
	when you want TLS with the native protocol; HTTPS implies TLS automatically.
- Provide CA bundles or client certificates via the uploader configuration when mutual TLS is
	required. If you pass only a username/password in the DSN the client continues to use basic auth.
- You can list multiple endpoints per flag to fan out batches to different clusters.

### 2. Configure the collector

The collector continues to scrape and buffer data in WAL segments, but it must tag those segments for
ClickHouse before forwarding them to the ingestor. Set `storage-backend = "clickhouse"` in the TOML
config (or pass `--storage-backend=clickhouse`) and keep the ingest endpoint pointed at the ingestor.

Example snippet derived from [`tools/clickhouse/collector-minimal.toml`](../tools/clickhouse/collector-minimal.toml):

```toml
endpoint = "https://ingestor:9090"
insecure-skip-verify = true
storage-backend = "clickhouse"
storage-dir = "/var/lib/adx-mon/collector"

[[prometheus-remote-write]]
database = "observability"
path = "/receive"

[[otel-metric]]
database = "observability"
path = "/v1/metrics"
grpc-port = 4317

[[host-log]]
disable-kube-discovery = true

	[[host-log.journal-target]]
	matches = ["_SYSTEMD_UNIT=docker.service"]
	database = "observability_logs"
	table = "docker_journal"
```

Restart the collector after updating the config so it loads the new backend mapping.

### 3. Validate ingestion

Once both components are running, send a test signal (for example, via OTLP/HTTP or OTLP/gRPC) and
confirm the data arrives:

```sh
clickhouse-client --host clickhouse.internal \
	--query "SELECT name, value::Float64, labels FROM observability.metrics_samples ORDER BY timestamp DESC LIMIT 10"
```

If you enabled TLS via `secure=1`, include `--secure` and the appropriate certificate flags when
running `clickhouse-client`.

### 4. Use the dev harness (optional but recommended)

`./tools/clickhouse/dev_stack.sh start` spins up ClickHouse, the ingestor, and the collector on a
single Docker network. It applies the settings described above, seeds the schema, exposes the Tabix UI
at [http://localhost:8123/play](http://localhost:8123/play), and mounts WAL directories under
`tools/clickhouse/.stack/`. See the [README](../tools/clickhouse/README.md) for day-two commands such
as `stop`, `cleanup`, and log streaming.

### Troubleshooting tips

- If the ingestor logs `remote error: tls: handshake failure`, double-check that your DSN requested
	TLS (`https://` or `secure=1`) and that the ClickHouse server is actually serving TLS on that port.
- Missing data typically means the ingestor dispatch queue is empty—verify that the collector is
	targeting the same database name you provided to the ClickHouse uploader.
- Use `./tools/clickhouse/dev_stack.sh logs ingestor` to watch the pipeline in real time during local
	experiments.
