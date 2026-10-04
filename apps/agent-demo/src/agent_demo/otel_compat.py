"""Quarantine for OpenTelemetry Python APIs that are still underscored.

Two things this demo needs have no public import path yet in
opentelemetry-python 1.43:

- The logs API/SDK (``opentelemetry._logs``, ``opentelemetry.sdk._logs``).
  The dedicated Events API was deprecated in 1.39; the sanctioned way to emit
  an event is a ``LogRecord`` with the top-level ``event_name`` field
  (available since 1.35), which is exactly what the GenAI conventions'
  ``gen_ai.evaluation.result`` event is.
- The OTLP logs file encoder (``..._log_encoder``); its trace and metric
  siblings are public.

Every underscore import lives here so an upstream rename is a one-file fix
and the rest of the codebase only sees stable names.
"""

import time

from opentelemetry._logs import LogRecord, SeverityNumber
from opentelemetry.exporter.otlp.proto.common._log_encoder import encode_logs
from opentelemetry.sdk._logs import LoggerProvider
from opentelemetry.sdk._logs.export import InMemoryLogRecordExporter, SimpleLogRecordProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.trace import TraceFlags

# Stable local alias: the SDK renamed InMemoryLogExporter to
# InMemoryLogRecordExporter and deprecation-warns on the old name.
InMemoryLogExporter = InMemoryLogRecordExporter

__all__ = [
    "InMemoryLogExporter",
    "LoggerProvider",
    "encode_logs",
    "new_logger_provider",
    "emit_event",
]


def new_logger_provider(resource: Resource, exporter: InMemoryLogExporter) -> LoggerProvider:
    """Build a LoggerProvider that hands every record to the exporter."""
    provider = LoggerProvider(resource=resource)
    provider.add_log_record_processor(SimpleLogRecordProcessor(exporter))
    return provider


def emit_event(
    provider: LoggerProvider,
    event_name: str,
    attributes: dict[str, str | int | float | bool],
    trace_id_hex: str | None = None,
    span_id_hex: str | None = None,
) -> None:
    """Emit one event as a LogRecord, optionally linked to a span.

    The GenAI conventions say an evaluation event SHOULD be parented to the
    span being evaluated; the hex ids come back from the demo's /ask API.
    """
    logger = provider.get_logger("agent_demo.events", "0.1.0")
    logger.emit(
        LogRecord(
            timestamp=time.time_ns(),
            observed_timestamp=time.time_ns(),
            trace_id=int(trace_id_hex, 16) if trace_id_hex else None,
            span_id=int(span_id_hex, 16) if span_id_hex else None,
            trace_flags=TraceFlags(TraceFlags.SAMPLED) if trace_id_hex else None,
            severity_text="INFO",
            severity_number=SeverityNumber.INFO,
            body=None,
            attributes=attributes,
            event_name=event_name,
        )
    )
