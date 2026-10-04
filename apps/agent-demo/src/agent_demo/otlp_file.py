"""Offline OTLP export: telemetry to .binpb files for the CI gate.

Protobuf binary is the interchange format on purpose: protobuf's canonical
JSON encodes trace/span ids as base64 while the OTLP/JSON spec wants hex —
binary bytes have no such ambiguity (ADR-0005 in the repo docs). The Go
side reads these with ExportRequest.UnmarshalProto.
"""

from pathlib import Path

from opentelemetry.exporter.otlp.proto.common.metrics_encoder import encode_metrics
from opentelemetry.exporter.otlp.proto.common.trace_encoder import encode_spans

from agent_demo.otel_compat import InMemoryLogExporter, encode_logs
from agent_demo.telemetry import TelemetryBundle


def export_files(
    prefix: str | Path,
    bundle: TelemetryBundle,
    log_exporter: InMemoryLogExporter | None = None,
) -> list[Path]:
    """Write <prefix>.traces.binpb / .metrics.binpb / .logs.binpb."""
    if bundle.span_exporter is None or bundle.metric_reader is None:
        raise ValueError("export_files needs a memory-mode TelemetryBundle")

    prefix = Path(prefix)
    prefix.parent.mkdir(parents=True, exist_ok=True)
    bundle.force_flush()
    written: list[Path] = []

    spans = bundle.span_exporter.get_finished_spans()
    traces_path = prefix.with_name(prefix.name + ".traces.binpb")
    traces_path.write_bytes(encode_spans(spans).SerializeToString())
    written.append(traces_path)

    metrics_data = bundle.metric_reader.get_metrics_data()
    if metrics_data is not None:
        metrics_path = prefix.with_name(prefix.name + ".metrics.binpb")
        metrics_path.write_bytes(encode_metrics(metrics_data).SerializeToString())
        written.append(metrics_path)

    if log_exporter is not None:
        logs = list(log_exporter.get_finished_logs())
        if logs:
            logs_path = prefix.with_name(prefix.name + ".logs.binpb")
            logs_path.write_bytes(encode_logs(logs).SerializeToString())
            written.append(logs_path)

    return written
