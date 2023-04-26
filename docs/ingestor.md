# Ingestor Overview

The ingestor is an aggregation point for adx-mon to ingest metrics, logs and traces into Azure Data Explore (ADX)
in a performant and cost-efficient manner.

ADX recommends sending batches of data in 100MB to 1GB (uncompressed) [1]
for optimal ingestion and reduced costs.  In a typical Kubernetes cluster, there are many pods, each with
their own metrics, logs and traces.  The ingestor aggregates these data sources into batches and sends them to ADX
instead of each pod or node sending data individually.  This reduces the number of small files that ADX must later 
merge into larger files which can impact query latency and increase resource requirements.

In addition, ADX is 2x to 3x more efficient at ingesting CSV vs JSON data.  The ingestor converts metrics, logs and
traces into CSV format before sending them to ADX which reduces the processing time and improves ingestion latency.

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

[1] https://docs.microsoft.com/en-us/azure/data-explorer/ingest-best-practices
