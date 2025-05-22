# Ingestor Storage and Adaptive Failover Peering

## Overview

The Ingestor component of ADX-Mon uses a Write-Ahead Log (WAL) to buffer telemetry data (metrics, logs, traces) before uploading to Azure Data Explorer (ADX). To prevent disk exhaustion, the ingestor enforces a configurable `max-disk-usage` limit. When this limit is reached, new data is rejected until space is freed.

## Adaptive Failover Peering Strategy

### Motivation

In multi-database and multi-cluster environments, a failure or unavailability of a single ADX database should not block ingestion for healthy databases. The adaptive failover peering strategy ensures that only the affected database's ingestion is blocked, while others continue to operate normally.

### How It Works

- **Normal Operation:**
  - Each ingestor instance participates in a peer group, sharding WAL segments by database and table.
  - Segments are transferred to the peer responsible for their database/table, coalesced, and uploaded to ADX.
  - The `max-disk-usage` limit is enforced globally per ingestor instance.

- **Database Failover:**
  - When a database becomes unavailable (e.g., ADX cluster or DB outage), the system enters failover mode for that database.
  - All WAL segments for the failed database are routed to a single designated "sacrificial" ingestor instance.
  - Other ingestor instances continue normal operation for healthy databases.

- **Sacrificial Instance Behavior:**
  - The sacrificial instance buffers all segments for the failed database until its `max-disk-usage` is reached.
  - Once full, it returns HTTP 429 (Too Many Requests) to peer transfer requests for that database.
  - Other peers, upon receiving 429s for failover segments, drop those segments and log the event (with optional metrics).
  - Ingestion for the failed database is effectively paused, but healthy databases are unaffected.

### Operational Impact

- **Isolation:** Only the database in failover is blocked; others continue ingesting and uploading.
- **Simplicity:** No per-database quota enforcement is needed; the failover mechanism is adaptive and peer-driven.
- **Observability:** Dropped segments and failover events are logged, and can be tracked via metrics.

### Recovery

- When the failed database becomes healthy again, failover mode is cleared and normal peer sharding resumes.
- Any segments dropped during failover are not recoverable, but WAL segments for healthy databases are preserved and uploaded as usual.

## Summary

The adaptive failover peering strategy provides robust, database-level isolation for ingestion failures, ensuring that outages or backpressure in one ADX database do not cascade to others. This approach is simple to operate and integrates seamlessly with the ingestor's WAL and peer transfer logic.
