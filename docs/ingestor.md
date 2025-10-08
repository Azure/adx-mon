# Ingestor Overview

The ingestor is an aggregation point for adx-mon to ingest metrics, logs and traces into Azure Data Explore (ADX)
in a performant and cost-efficient manner.

ADX recommends sending batches of data in 100MB to 1GB (uncompressed) [1]
for optimal ingestion and reduced costs.  In a typical Kubernetes cluster, there are many pods, each with
their own metrics, logs and traces.  The ingestor aggregates these data sources into batches and sends them to ADX
instead of each pod or node sending data individually.  This reduces the number of small files that ADX must later 
merge into larger files which can impact query latency and increase resource requirements.

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

## Autoscaling

The operator ships with a purpose-built autoscaler that adjusts the StatefulSet replica count based on the average CPU
utilization of the nodes currently hosting ingestor pods. During each reconciliation cycle the operator collects
metrics from the Kubernetes `metrics.k8s.io` API, retains a sliding window of samples, and decides whether to add or
remove replicas. Scale-downs are coordinated using `shutdown-requested`/`shutdown-completed` pod annotations so that a
pod can gracefully flush its WAL before deletion.

Autoscaling is disabled by default. To enable it, add an `autoscaler` block to your `Ingestor` custom resource:

```yaml
apiVersion: adx-mon.azure.com/v1
kind: Ingestor
metadata:
  name: adx-mon
  namespace: adx-mon
spec:
  replicas: 3
  autoscaler:
    enabled: true
    minReplicas: 3
    maxReplicas: 12
    scaleUpCPUThreshold: 70     # scale up when rolling average CPU exceeds 70%
    scaleDownCPUThreshold: 40   # consider scale down at or below 40%
    scaleInterval: 5m           # enforce at least 5 minutes between scale actions
    cpuWindow: 15m              # size of the rolling metrics window
    scaleUpBasePercent: 25      # grow by 25% (capped by scaleUpCapPerCycle) per step
    scaleUpCapPerCycle: 5       # prevent large jumps in a single iteration
    dryRun: false               # set true to observe decisions without changing replicas
```

Key behaviors and configuration options:

* `minReplicas`/`maxReplicas` bound the replica range. Defaults are derived from `spec.replicas` when omitted.
* CPU thresholds are expressed as percentages. You can widen the hysteresis band to tolerate more variance.
* `scaleInterval` controls the minimum time between scaling actions. Scale intervals shorter than the metrics window
  will be clamped by the autoscaler.
* `scaleUpBasePercent` determines the proportion of the current replica count added during a scale-up. The value is
  capped by `scaleUpCapPerCycle` to avoid overshooting in large clusters.
* Enable `dryRun` to surface decisions through status updates and events without mutating the StatefulSet. This is
  useful when calibrating thresholds.
* `collectMetrics` (default `true`) is reserved for future integrations that provide external metrics sources. Leave it
  enabled unless you have a replacement data path wired into the operator.

The autoscaler records its last decision under `status.autoscaler`. The status includes the most recent action, the
time it occurred, and the last observed CPU utilization (stored as a percentage multiplied by 100). This makes it easy
to visualize the control loop with `kubectl get ingestor -o yaml` or prometheus rules.

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

## WAL Format and Storage

The Ingestor uses a Write-Ahead Log (WAL) for durable, append-only buffering of telemetry data before upload to Azure Data Explorer. The WAL binary format is fully documented in [Concepts: WAL Segment File Format](concepts.md#wal-segment-file-format), including:
- Segment and block header layout
- Field encoding and versioning
- Compression (S2/Snappy)
- Repair and compatibility

For advanced troubleshooting, integrations, or recovery, see the [WAL format section](concepts.md#wal-segment-file-format) and the implementation in `pkg/wal/segment.go`.

[1] https://docs.microsoft.com/en-us/azure/data-explorer/ingest-best-practices
