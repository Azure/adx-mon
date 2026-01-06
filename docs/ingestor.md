# Ingestor Overview

The ingestor is an aggregation point for adx-mon to ingest metrics, logs and traces into Azure Data Explore (ADX)
in a performant and cost-efficient manner.

ADX recommends sending batches of data in 100MB to 1GB (uncompressed) [1]
for optimal ingestion and reduced costs.  In a typical Kubernetes cluster, there are many pods, each with
their own metrics, logs and traces.  The ingestor aggregates these data sources into batches and sends them to ADX
instead of each pod or node sending data individually.  This reduces the number of small files that ADX must later 
merge into larger files which can impact query latency and increase resource requirements.

# Deployment

## Operator-Managed Deployment

The adx-mon operator provides a declarative way to deploy the ingestor using the `Ingestor` CRD. This is the intended future path for managing ingestor deployments. The operator handles:
- StatefulSet creation with proper security contexts
- Service and RBAC configuration
- Automatic connection to ADXCluster resources via label selectors
- Installation of dependent CRDs (Function, SummaryRule, ManagementCommand)
- Rolling updates when the CRD spec changes

**Example:**
```yaml
apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: prod-ingestor
  namespace: monitoring
spec:
  replicas: 3
  adxClusterSelector:
    matchLabels:
      env: production
  # Optional: skip reconciliation on non-production clusters
  criteriaExpression: "environment == 'prod'"
```

See the [Ingestor CRD Reference](crds.md#ingestor) for full field documentation and the [Operator Design](designs/operator.md#ingestor-crd) for implementation details.

## Manual Deployment

For advanced use cases, the ingestor can be deployed directly as a StatefulSet. See the [Configuration](config.md) page for CLI flags and environment variables.

# Design

The ingestor is designed to be deployed as a Kubernetes StatefulSet with multiple replicas.  It exposes several
ingress points for metrics, logs and traces collection.  The metrics ingress API is a Prometheus remote write endpoint and can support
other interfaces such as OpenTelemetry in the future.  

The ingestor can be dynamically scaled up or down based on the amount of data being ingested.  It has a configurable
amount of storage to buffer data before sending it to ADX.  It will store and coalesce data until it reaches a
maximum size or a maximum age.  Once either of these thresholds are reached, the data is sent to ADX.

Several design decisions were made to optimize availability and performance.  For example, if a pod is able to recieve data
it will store it locally and attempt to optimize it for upload to ADX later by transferring small segments to peers.
The performance and throughput of a single ingestor pod is limited by network bandwidth and disk throughput of attached 
storage.  The actual processing performed by the ingestor is fairly minimal and is mostly unmarshalling
the incoming data (Protobufs) and writing it to disk in an append only storage format.

# Data Flow

## Metrics

Each ingestor pod is fronted by a load balancer.  The ingestor receives data from the Kubernetes cluster via the 
Prometheus remote write endpoint.  When a pod receives data, it writes it locally to a file that
corresponds to a given table and schema.  These files are called Segments and are part of Write Ahead Log (WAL)
for each table. 

If Segment has reached the max age or max size, the ingestor will either upload the file directly to ADX or
transfer the file to a peer that is assigned to own that particular table.  The transfer is performed if the file
is less than 100MB so that the file can be merged with other files before being uploaded to ADX.  

If the transfer fails, the instance will upload the file directly. 

During upload, batches of files, per table, are compressed and uploaded to ADX as stream.  This allows many small
files to be merged into a single file which reduces the number of files that ADX must merge later.  Each batch is
sized to be between 100MB and 1GB (uncompressed) to align with Kusto ingestion best practices.

## Logs

## Traces

## ClickHouse sink

The ingestor can stream batches to ClickHouse in addition to Azure Data Explorer. Switch the storage
backend to `clickhouse` when you want to land telemetry in a ClickHouse cluster—either for hybrid
deployments or for local development.

### Configure the ingestor

1. Launch the binary with `--storage-backend=clickhouse` (or set
	`INGESTOR_STORAGE_BACKEND=clickhouse`).
2. Provide one or more metrics and logs endpoints with the existing
	`--metrics-kusto-endpoints` / `--logs-kusto-endpoints` flags. Each value uses the familiar
	`<database>=<dsn>` format. Example:

	```sh
	--metrics-kusto-endpoints "observability=clickhouse://default:devpass@clickhouse:9000/observability"
	--logs-kusto-endpoints "observability_logs=clickhouse://default:devpass@clickhouse:9000/observability_logs"
	```

	The ingestor automatically provisions the required tables (`metrics_samples` and `otel_logs`) and
	maps lifted labels/resources to ClickHouse columns.
3. TLS is disabled unless the DSN explicitly requests it. Use either an HTTPS-based DSN or append
	`secure=1`/`secure=true` when using the native protocol. Optional certificates can be supplied via
	the ClickHouse uploader configuration (CA, client cert/key, or `InsecureSkipVerify`).

> **Tip:** You can target multiple ClickHouse clusters by repeating the endpoint flags; the ingestor
> fans out batches to every configured DSN for a given stream.

### Align the collector

Collect the same WAL format by setting the collector configuration to `storage-backend = "clickhouse"`
(or pass the `--storage-backend` CLI flag). No other configuration changes are required—the collector
still delivers segments to the ingestor over the transfer API.

### Local harness

The helper script in `tools/clickhouse/dev_stack.sh` spins up a complete collector → ingestor →
ClickHouse pipeline on Docker. It builds fresh images (unless `SKIP_BUILD=1`), launches a ClickHouse
server with pre-created `observability` and `observability_logs` databases, and wires the collector to
the ingestor using the clickhouse backend. See [`tools/clickhouse/README.md`](../tools/clickhouse/README.md)
for usage, including how to seed OTLP metrics and query the data with `clickhouse-client` or the Tabix
UI at `http://localhost:8123/play`.

## WAL Format and Storage

The Ingestor uses a Write-Ahead Log (WAL) for durable, append-only buffering of telemetry data before upload to Azure Data Explorer. The WAL binary format is fully documented in [Concepts: WAL Segment File Format](concepts.md#wal-segment-file-format), including:
- Segment and block header layout
- Field encoding and versioning
- Compression (S2/Snappy)
- Repair and compatibility

For advanced troubleshooting, integrations, or recovery, see the [WAL format section](concepts.md#wal-segment-file-format) and the implementation in `pkg/wal/segment.go`.

[1] https://docs.microsoft.com/en-us/azure/data-explorer/ingest-best-practices
