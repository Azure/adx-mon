"""
Custom OpenTelemetry span exporter that converts spans to OTLP logs.
"""

import os
import time
from typing import Sequence, Optional, Any, Dict
from datetime import datetime

from opentelemetry.sdk.trace import ReadableSpan
from opentelemetry.sdk.trace.export import SpanExporter, SpanExportResult
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry._logs import SeverityNumber


class SpanToLogExporter(SpanExporter):
  """
  A custom span exporter that converts spans to OTLP logs and sends them via HTTP.
  """

  def __init__(self, endpoint: Optional[str] = None):
    """
    Initialize the span-to-log exporter.
    Args:
      endpoint: The OTLP HTTP endpoint. If not provided, uses NODE_IP from environment
          or defaults to http://127.0.0.1:9091/v1/logs
    """
    if endpoint is None:
      node_ip = os.environ.get("NODE_IP", "127.0.0.1")
      endpoint = f"http://{node_ip}:9091/v1/logs"
    
    # Create OTLP log exporter
    self._log_exporter = OTLPLogExporter(endpoint=endpoint)
    # Create logger provider
    self._logger_provider = LoggerProvider()
    self._logger_provider.add_log_record_processor(
      BatchLogRecordProcessor(self._log_exporter)
    )
    # Get logger
    self._logger = self._logger_provider.get_logger("span-to-log-exporter")
    print(f"SpanToLogExporter initialized, sending to: {endpoint}")

  def export(self, spans: Sequence[ReadableSpan]) -> SpanExportResult:
    """
    Export spans by converting them to logs.
    Args:
      spans: The spans to export
    Returns:
      SpanExportResult indicating success or failure
    """
    try:
      for span in spans:
        # Convert span to structured data
        span_data = self._convert_span_to_dict(span)
        
        # Emit log with kusto routing attributes
        self._logger.emit(
          self._create_log_record(
            body=span_data,
            timestamp=span.end_time,
            attributes={
              "kusto.database": "Logs",
              "kusto.table": "Traces",
              "span.name": span.name,
              "trace.id": format(span.context.trace_id, "032x"),
              "span.id": format(span.context.span_id, "016x"),
              "span.kind": span.kind.name,
            }
          )
        )
      
      # Force flush to ensure logs are sent
      self._logger_provider.force_flush()
      return SpanExportResult.SUCCESS
    except Exception as e:
      print(f"Error exporting spans: {e}")
      return SpanExportResult.FAILURE

  def _convert_span_to_dict(self, span: ReadableSpan) -> Dict[str, Any]:
    """Convert a span to a dictionary structure."""
    span_data = {
      "Name": span.name,
      "SpanContext": {
        "TraceID": format(span.context.trace_id, "032x"),
        "SpanID": format(span.context.span_id, "016x"),
        "TraceFlags": format(span.context.trace_flags, "02x"),
      },
      "SpanKind": span.kind.name,
      "StartTime": self._format_timestamp(span.start_time),
      "EndTime": self._format_timestamp(span.end_time),
    }
   
    # Add parent context if available
    if span.parent:
      span_data["Parent"] = {
        "TraceID": format(span.parent.trace_id, "032x"),
        "SpanID": format(span.parent.span_id, "016x"),
        "TraceFlags": format(span.parent.trace_flags, "02x"),
      }
    else:
      span_data["Parent"] = {
        "TraceID": "00000000000000000000000000000000",
        "SpanID": "0000000000000000",
        "TraceFlags": "00",
      }
    
    # Add attributes
    if span.attributes:
      span_data["Attributes"] = dict(span.attributes)
   
    # Add events
    if span.events:
      events = []
      for event in span.events:
        event_dict = {
          "Name": event.name,
          "Time": self._format_timestamp(event.timestamp),
        }
        if event.attributes:
          event_dict["Attributes"] = dict(event.attributes)
        events.append(event_dict)
      span_data["Events"] = events
    
    # Add status
    span_data["Status"] = {
      "Code": span.status.status_code.name,
      "Description": span.status.description or "",
    }
    
    # Add resource attributes
    if span.resource and span.resource.attributes:
      span_data["Resource"] = dict(span.resource.attributes)
    return span_data

  def _format_timestamp(self, timestamp_ns: int) -> str:
    """Format a nanosecond timestamp to RFC3339Nano string."""
    timestamp_s = timestamp_ns / 1_000_000_000
    dt = datetime.utcfromtimestamp(timestamp_s)
    # Format with nanosecond precision
    ns = timestamp_ns % 1_000_000_000
    return dt.strftime("%Y-%m-%dT%H:%M:%S") + f".{ns:09d}Z"

  def _create_log_record(self, body: Any, timestamp: int, attributes: Dict[str, str]):
    """
    Create a log record with structured body.
    The body is kept as a dictionary to preserve the object structure.
    """
    from opentelemetry._logs import LogRecord
    # Create log record with structured body (dict, not string)
    log_record = LogRecord(
      timestamp=timestamp,
      body=body,  # Keep as dict for structured logging
      severity_number=SeverityNumber.INFO,
      severity_text="INFO",
      attributes=attributes,
    )
    return log_record

  def shutdown(self) -> None:
    """Shutdown the exporter and flush any pending logs."""
    if self._logger_provider:
      self._logger_provider.shutdown()

  def force_flush(self, timeout_millis: int = 30000) -> bool:
    """Force flush any pending logs."""
    if self._logger_provider:
      return self._logger_provider.force_flush(timeout_millis // 1000)
    return True
