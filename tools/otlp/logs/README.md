# About

This directory contains artifacts useful for integration testing OTLP logs.
In particular, this scenario creates a logs emitter via fluent-bit, which exports
logs via OTLP over HTTP. We then proxy fluent-bit's requests from Collector to
opentelemetry-collector by upgrading the connection to gRPC. Later, we can replace
opentelemetry-collector with Ingestor.

## Testing

```bash
docker compose -f tools/otlp/logs/compose.yaml build
docker compose -f tools/otlp/logs/compose.yaml up
```

## Kusto

[Kustainer](https://learn.microsoft.com/en-us/azure/data-explorer/kusto-emulator-install) is a container that
runs Kusto in volatile mode. Logs are uploaded to Kustainer and can be inspected by connecting to `http://localhost:8081`, be sure to set `Security` to `Client Security: None` in the connection dialog. From there you can
inspect logs as uploaded by _Ingestor_ as consumed by _Collector_ and published by _Fluent_ as defined by _fluent.yaml_.