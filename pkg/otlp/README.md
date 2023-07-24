# About

This package implements an OTLP proxy that can receive HTTP/JSON or HTTP/Protobuf requests and forward them to an OTLP gRPC endpoint.

## Integration Testing

An easy test is to use _fluent-bit_ to _Collector_ to _opentelemetry-collector_ via _docker-compose_. From the root of the repository, run:

```bash
docker compose -f tools/otlp/logs/compose.yaml build
docker compose -f tools/otlp/logs/compose.yaml up
```
