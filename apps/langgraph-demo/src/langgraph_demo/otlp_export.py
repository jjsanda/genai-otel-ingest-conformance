"""Telemetry pipeline setup and offline OTLP export.

Two modes, chosen once at startup:

- **memory** (default, and always for ``--otlp-out``): spans and metrics
  collect in-process and are written as OTLP protobuf files. Protobuf binary
  is the cross-language interchange format on purpose — protobuf's canonical
  JSON encodes trace/span ids as base64 while the OTLP/JSON spec wants hex;
  binary bytes have no such ambiguity (ADR-0005). The Go conformance CLI
  reads these files directly.
- **OTLP/HTTP** (``OTEL_EXPORTER_OTLP_ENDPOINT`` set): the standard live
  export used by the docker-compose and kind stacks.

The MeterProvider applies the conventions' advisory bucket boundaries via
Views so ``gen_ai.client.*`` histograms are comparable across services —
exactly what conformance rule GENAI-METRIC-004 checks for.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

from opentelemetry.exporter.otlp.proto.common.metrics_encoder import encode_metrics
from opentelemetry.exporter.otlp.proto.common.trace_encoder import encode_spans
from opentelemetry.metrics import Histogram
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader, PeriodicExportingMetricReader
from opentelemetry.sdk.metrics.view import ExplicitBucketHistogramAggregation, View
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import Tracer

SERVICE_NAME = "demo-langgraph-agent"
SERVICE_VERSION = "0.1.0"

# ExplicitBucketBoundaries advice from the pinned conventions
# (docs/gen-ai/gen-ai-metrics.md); mirrored by internal/registry/curated/.
TOKEN_BUCKETS = [
    1,
    4,
    16,
    64,
    256,
    1024,
    4096,
    16384,
    65536,
    262144,
    1048576,
    4194304,
    16777216,
    67108864,
]
DURATION_BUCKETS = [
    0.01,
    0.02,
    0.04,
    0.08,
    0.16,
    0.32,
    0.64,
    1.28,
    2.56,
    5.12,
    10.24,
    20.48,
    40.96,
    81.92,
]


@dataclass
class Telemetry:
    """The demo's telemetry pipeline handles."""

    tracer: Tracer
    token_usage: Histogram
    operation_duration: Histogram
    tracer_provider: TracerProvider
    meter_provider: MeterProvider
    span_exporter: InMemorySpanExporter | None
    metric_reader: InMemoryMetricReader | None

    def export_files(self, prefix: str | Path) -> list[Path]:
        """Write <prefix>.traces.binpb and <prefix>.metrics.binpb."""
        if self.span_exporter is None or self.metric_reader is None:
            raise ValueError("file export requires memory mode")
        prefix = Path(prefix)
        prefix.parent.mkdir(parents=True, exist_ok=True)
        self.tracer_provider.force_flush()

        written: list[Path] = []
        traces_path = prefix.with_name(prefix.name + ".traces.binpb")
        traces_path.write_bytes(
            encode_spans(self.span_exporter.get_finished_spans()).SerializeToString()
        )
        written.append(traces_path)

        metrics_data = self.metric_reader.get_metrics_data()
        if metrics_data is not None:
            metrics_path = prefix.with_name(prefix.name + ".metrics.binpb")
            metrics_path.write_bytes(encode_metrics(metrics_data).SerializeToString())
            written.append(metrics_path)
        return written

    def shutdown(self) -> None:
        self.tracer_provider.shutdown()
        self.meter_provider.shutdown()


def setup_telemetry(memory: bool) -> Telemetry:
    """Build local (never global) tracer and meter providers."""
    resource = Resource.create({"service.name": SERVICE_NAME, "service.version": SERVICE_VERSION})
    views = [
        View(
            instrument_name="gen_ai.client.token.usage",
            aggregation=ExplicitBucketHistogramAggregation(TOKEN_BUCKETS),
        ),
        View(
            instrument_name="gen_ai.client.operation.duration",
            aggregation=ExplicitBucketHistogramAggregation(DURATION_BUCKETS),
        ),
    ]

    span_exporter: InMemorySpanExporter | None = None
    metric_reader: InMemoryMetricReader | None = None
    if memory:
        span_exporter = InMemorySpanExporter()
        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        metric_reader = InMemoryMetricReader()
        meter_provider = MeterProvider(
            resource=resource, metric_readers=[metric_reader], views=views
        )
    else:
        # Lazy imports: the OTLP HTTP exporters are only needed in live mode.
        from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
        meter_provider = MeterProvider(
            resource=resource,
            metric_readers=[PeriodicExportingMetricReader(OTLPMetricExporter())],
            views=views,
        )

    tracer = tracer_provider.get_tracer("langgraph_demo.bridge")
    meter = meter_provider.get_meter("langgraph_demo.bridge")
    token_usage = meter.create_histogram(
        "gen_ai.client.token.usage",
        unit="{token}",
        description="Number of input and output tokens used.",
    )
    operation_duration = meter.create_histogram(
        "gen_ai.client.operation.duration", unit="s", description="GenAI operation duration."
    )
    return Telemetry(
        tracer=tracer,
        token_usage=token_usage,
        operation_duration=operation_duration,
        tracer_provider=tracer_provider,
        meter_provider=meter_provider,
        span_exporter=span_exporter,
        metric_reader=metric_reader,
    )


def live_mode_requested() -> bool:
    return bool(os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"))
