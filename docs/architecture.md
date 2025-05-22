# Architecture

## Resilience and Partitioning

ADX-Mon is designed for high availability and fault isolation. The ingestor's adaptive failover peering strategy ensures that failures or outages in a single ADX database do not impact ingestion for other databases. When a database is unavailable, only that database's ingestion is blocked (via a "sacrificial" peer and segment dropping), while healthy databases continue to ingest and upload data normally.

This approach provides robust isolation between workloads and simplifies operational recovery.

# ...existing content...
