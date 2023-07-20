# About

This package implements an OTLP proxy that can receive HTTP/JSON or HTTP/Protobuf requests and forward them to an OTLP gRPC endpoint.

## Integration Testing

An easy test is to use _fluent-bit_ to _Collector_ to _opentelemetry-collector_ via _docker-compose_.

compose.yaml

```yaml
version: "3"
services:
  fluentbit:
    image: cr.fluentbit.io/fluent/fluent-bit
    command: -c /etc/fluent-bit/config.yaml
    volumes:
      - ./fluent.yaml:/etc/fluent-bit/config.yaml
    depends_on:
      - collector

  otel:
    image: otel/opentelemetry-collector-contrib
    volumes:
      - ./otlp.yaml:/etc/otelcol-contrib/config.yaml
    ports:
      - 4317:4317

  collector:
    build:
      dockerfile: ../Dockerfile
      context: ./adx-mon
    volumes:
      - ~/.kube/config:/root/.kube/config
    ports:
      - 8080:8080
    # While the _endpoints_ URI doesn't match the expected OTLP syntax, keep in
    # mind that _Collector_ is currently configured to connect to _Ingestor_, so we
    # just modify the _endpoints_ in pkg/otlp/proxy to be OTLP compliant.
    command: --kubeconfig /root/.kube/config --endpoints http://otel:4317/receive --insecure-skip-verify
    depends_on:
      - otel
    environment:
      - LOG_LEVEL=DEBUG

```

fluent.yaml

```yaml
service:
  flush: 1
  log_level: debug

pipeline:

  inputs:
    - name: dummy
      tag: dummy
      dummy: {"message": "hello world"}

  outputs:
    - Name: opentelemetry
      Match: "*"
      Host: collector
      Port: 8080
      Logs_uri: /logs
      Log_response_payload: true
      TLS: false
      TLS.verify: false
```

otlp.yaml

```yaml
receivers:
  otlp:
    protocols:
      grpc:

exporters:
  logging:
    verbosity: "detailed"

service:
  telemetry:
    logs:
      level: "debug"

  pipelines:
    logs:
      receivers: [otlp]
      exporters: [logging]
```

Then run `docker-compose build && docker-compose up` to start the services and observe the traffic.
