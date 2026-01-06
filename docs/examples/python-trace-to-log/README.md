# Python Trace Example

This example demonstrates how to send OpenTelemetry traces as OTLP logs to the ADX-Mon collector.

## Features

- Creates OpenTelemetry traces with spans
- Custom span processor that converts spans to OTLP logs
- Sends logs to the collector via OTLP/HTTP
- Sets `kusto.database` and `kusto.table` attributes for routing

## Installation

```bash
pip install -r requirements.txt
```

## Usage

Set the `NODE_IP` environment variable to the collector's IP address:

```bash
export NODE_IP=127.0.0.1  # or your collector's IP
python trace_example.py
```

The example will:
1. Initialize OpenTelemetry tracing with the custom span-to-log exporter
2. Create sample traces with nested spans
3. Send the span data as structured OTLP logs to `http://${NODE_IP}:9091/v1/logs`
4. Each log will have attributes `kusto.database=Logs` and `kusto.table=Traces`
