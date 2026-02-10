#!/usr/bin/env python3
"""
Example Python application that generates OpenTelemetry traces and exports them as OTLP logs.
"""

import os
import time
import random
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.semconv.resource import ResourceAttributes

from span_to_log_exporter import SpanToLogExporter


def setup_tracing():
  """Initialize OpenTelemetry tracing with the span-to-log exporter."""
  # Create resource with service information
  resource = Resource.create({
    ResourceAttributes.SERVICE_NAME: "python-trace-example",
    ResourceAttributes.SERVICE_VERSION: "1.0.0",
  })
  # Create tracer provider
  provider = TracerProvider(resource=resource)
  # Create and configure the span-to-log exporter
  node_ip = os.environ.get("NODE_IP", "127.0.0.1")
  exporter = SpanToLogExporter(endpoint=f"http://{node_ip}:9091/v1/logs")
  # Add batch span processor
  processor = BatchSpanProcessor(exporter)
  provider.add_span_processor(processor)
  # Set as global tracer provider
  trace.set_tracer_provider(provider)
  print(f"Tracing initialized, sending to http://{node_ip}:9091/v1/logs")
  return provider

def simulate_work(duration_ms):
  """Simulate some work by sleeping."""
  time.sleep(duration_ms / 1000.0)

def process_request(tracer, request_id):
  """Simulate processing a request with nested spans."""
  with tracer.start_as_current_span(
    "process_request",
    attributes={
      "request.id": request_id,
      "http.method": "POST",
      "http.url": "/api/process",
    }
  ) as parent_span:
    parent_span.add_event("request_received", {"timestamp": time.time()})
    # Simulate validation
    with tracer.start_as_current_span(
      "validate_request",
      attributes={"validator": "schema_v1"}
    ) as validate_span:
      simulate_work(50)
      validate_span.add_event("validation_complete", {"valid": True})
    # Simulate database query
    with tracer.start_as_current_span(
      "database_query",
      attributes={
        "db.system": "postgresql",
        "db.statement": "SELECT * FROM users WHERE id = ?",
        "db.name": "production",
      }
    ) as db_span:
      simulate_work(100)
      db_span.add_event("query_executed", {"rows_returned": 1})
    # Simulate processing
    with tracer.start_as_current_span(
      "process_data",
      attributes={"processor": "v2"}
    ) as process_span:
      simulate_work(75)
      process_span.add_event("processing_started")
      # Nested operation
      with tracer.start_as_current_span(
        "transform_data",
        attributes={"transformer": "json_to_proto"}
      ) as transform_span:
        simulate_work(30)
        transform_span.add_event("transformation_complete")
      process_span.add_event("processing_complete", {"records_processed": 42})
    # Simulate response preparation
    with tracer.start_as_current_span(
      "prepare_response",
      attributes={"response.format": "json"}
    ) as response_span:
      simulate_work(25)
      response_span.add_event("response_ready", {"status_code": 200})
    parent_span.add_event("request_complete", {
      "duration_ms": 280,
      "status": "success"
    })
    print(f"Request {request_id} processed - Trace ID: {format(parent_span.get_span_context().trace_id, '032x')}")


def main():
  """Main function to run the trace example."""
  print("Python Trace Example - Sending traces as OTLP logs")
  print("=" * 60)
  # Setup tracing
  provider = setup_tracing()
  # Get tracer
  tracer = trace.get_tracer("python-trace-example", "1.0.0")
  # Generate sample traces
  print("\nGenerating sample traces...")
  print("-" * 60)
  for i in range(5):
    request_id = f"req-{i+1:03d}"
    process_request(tracer, request_id)
    # Random delay between requests
    time.sleep(random.uniform(0.5, 2.0))
  print("-" * 60)
  print("\nFlushing remaining traces...")
  # Force flush to ensure all spans are exported
  provider.force_flush()
  print("Done! All traces have been sent as OTLP logs.")
  # Shutdown
  provider.shutdown()


if __name__ == "__main__":
  main()
