"""OpenTelemetry setup and the demo's GenAI telemetry emitter.

Every span and metric shape lives here (and only here), transcribed from the
same pinned conventions snapshot the Go conformance registry uses. The
emitter's attribute logic is split into small hook methods precisely so
noncompliant.py can override individual hooks to break specific rules —
compliant and broken telemetry come from the same code path, which is what
makes the demo honest.
"""

import json
import time
from contextlib import contextmanager
from dataclasses import dataclass, field

from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader, PeriodicExportingMetricReader
from opentelemetry.sdk.metrics.view import ExplicitBucketHistogramAggregation, View
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import SpanKind, StatusCode
from opentelemetry.trace.span import Span

from agent_demo import genai_attrs as ga
from agent_demo import retrieval
from agent_demo.llm import ChatResponse, Provider
from agent_demo.tools import Tool

AGENT_NAME = "trip-planner"
AGENT_DESCRIPTION = "Plans trips using calculator and city-facts tools, grounded in a travel corpus"
SYSTEM_PROMPT = (
    "You are trip-planner, a travel assistant. Use the calculator and city_facts "
    "tools when they help, ground answers in retrieved notes, and keep answers short."
)

# Advisory ExplicitBucketBoundaries from the pinned gen-ai-metrics.md. The
# agentic duration metrics have no upstream advice; reusing the client
# duration buckets keeps their histograms comparable.
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


@dataclass
class TelemetryBundle:
    """Providers plus capture handles (present only in memory mode)."""

    tracer_provider: TracerProvider
    meter_provider: MeterProvider
    resource: Resource
    span_exporter: InMemorySpanExporter | None = None
    metric_reader: InMemoryMetricReader | None = None

    def force_flush(self) -> None:
        self.tracer_provider.force_flush()
        self.meter_provider.force_flush()


def build_telemetry(
    service_name: str = "agent-demo",
    service_version: str = "0.1.0",
    mode: str = "memory",
    noncompliant: bool = False,
) -> TelemetryBundle:
    """Build providers: mode "memory" captures in-process (tests, offline
    export); mode "otlp" ships to OTEL_EXPORTER_OTLP_ENDPOINT (compose/kind);
    mode "none" instruments without exporting (long-running server with no
    endpoint configured — memory capture there would grow without bound).
    """
    if noncompliant:
        # Intentionally broken: no service.name (the SDK falls back to
        # unknown_service:python → GENAI-RES-001) and no service.version
        # (→ GENAI-RES-002).
        resource = Resource.create({})
    else:
        resource = Resource.create(
            {"service.name": service_name, "service.version": service_version}
        )

    views = [
        View(
            instrument_name="gen_ai.client.token.usage",
            aggregation=ExplicitBucketHistogramAggregation(TOKEN_BUCKETS),
        ),
        View(
            instrument_name="gen_ai.client.operation.duration",
            aggregation=ExplicitBucketHistogramAggregation(DURATION_BUCKETS),
        ),
        View(
            instrument_name="gen_ai.invoke_agent.duration",
            aggregation=ExplicitBucketHistogramAggregation(DURATION_BUCKETS),
        ),
        View(
            instrument_name="gen_ai.execute_tool.duration",
            aggregation=ExplicitBucketHistogramAggregation(DURATION_BUCKETS),
        ),
    ]

    if mode == "none":
        # No processors and no readers: spans/metrics are created (so code
        # paths stay identical) but nothing accumulates.
        return TelemetryBundle(
            TracerProvider(resource=resource),
            MeterProvider(resource=resource, views=views),
            resource,
        )

    if mode == "memory":
        span_exporter = InMemorySpanExporter()
        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(SimpleSpanProcessor(span_exporter))
        metric_reader = InMemoryMetricReader()
        meter_provider = MeterProvider(
            resource=resource, metric_readers=[metric_reader], views=views
        )
        return TelemetryBundle(
            tracer_provider, meter_provider, resource, span_exporter, metric_reader
        )

    # Lazy imports: the OTLP exporters spin up threads we never want in tests.
    from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

    tracer_provider = TracerProvider(resource=resource)
    tracer_provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    meter_provider = MeterProvider(
        resource=resource,
        metric_readers=[
            PeriodicExportingMetricReader(OTLPMetricExporter(), export_interval_millis=5000)
        ],
        views=views,
    )
    return TelemetryBundle(tracer_provider, meter_provider, resource)


@dataclass
class ChatCall:
    """Mutable holder the chat context manager yields; the caller assigns the
    provider response before the span closes."""

    response: ChatResponse | None = None
    error_type: str | None = None


@dataclass
class AgentRun:
    """State collected across one invoke_agent span."""

    trace_id_hex: str = ""
    span_id_hex: str = ""
    input_tokens: int = 0
    output_tokens: int = 0
    finish_reasons: list[str] = field(default_factory=list)
    response_id: str = ""

    def add_usage(self, resp: ChatResponse) -> None:
        self.input_tokens += resp.input_tokens
        self.output_tokens += resp.output_tokens


class GenAIInstrumentation:
    """Compliant GenAI semconv emitter.

    Span kinds follow the conventions: inference and retrieval are CLIENT
    calls; invoke_agent and execute_tool run in-process and are INTERNAL —
    which is also why provider.name is required on chat spans but not on the
    agent span.
    """

    def __init__(
        self, bundle: TelemetryBundle, provider: Provider, capture_content: bool = False
    ) -> None:
        self.provider = provider
        self.capture_content = capture_content
        self.tracer = bundle.tracer_provider.get_tracer("agent_demo", "0.1.0")
        meter = bundle.meter_provider.get_meter("agent_demo", "0.1.0")
        self.token_usage = meter.create_histogram(
            "gen_ai.client.token.usage",
            unit="{token}",
            description="Number of input and output tokens used.",
        )
        self.op_duration = meter.create_histogram(
            "gen_ai.client.operation.duration", unit="s", description="GenAI operation duration."
        )
        self.agent_duration = meter.create_histogram(
            "gen_ai.invoke_agent.duration",
            unit="s",
            description="End-to-end duration of one agent invocation.",
        )
        self.tool_duration = meter.create_histogram(
            "gen_ai.execute_tool.duration",
            unit="s",
            description="Duration of a single tool execution.",
        )

    # ---- naming hooks (noncompliant.py overrides selected ones) ----------

    def chat_span_name(self) -> str:
        return f"{ga.OP_CHAT} {self.provider.default_model}"

    def agent_span_name(self) -> str:
        return f"{ga.OP_INVOKE_AGENT} {AGENT_NAME}"

    def tool_span_name(self, tool_name: str) -> str:
        return f"{ga.OP_EXECUTE_TOOL} {tool_name}"

    def retrieval_span_name(self) -> str:
        return f"{ga.OP_RETRIEVAL} {retrieval.DATA_SOURCE_ID}"

    # ---- attribute hooks --------------------------------------------------

    def on_chat_start(self, span: Span, input_messages: list[dict]) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_CHAT)
        span.set_attribute(ga.GEN_AI_PROVIDER_NAME, self.provider.name)
        span.set_attribute(ga.GEN_AI_REQUEST_MODEL, self.provider.default_model)
        span.set_attribute(ga.GEN_AI_REQUEST_TEMPERATURE, 0.2)
        span.set_attribute(ga.GEN_AI_REQUEST_MAX_TOKENS, 512)
        if self.provider.server_address is not None:
            # server.port always travels with server.address: emitting the
            # address alone violates the conventions (GENAI-SPAN-008).
            span.set_attribute(ga.SERVER_ADDRESS, self.provider.server_address)
            span.set_attribute(ga.SERVER_PORT, self.provider.server_port or 443)
        if self.capture_content:
            # Content capture is opt-in per the conventions; shapes follow the
            # upstream JSON Schemas (role + typed parts).
            span.set_attribute(
                ga.GEN_AI_SYSTEM_INSTRUCTIONS,
                json.dumps([{"type": "text", "content": SYSTEM_PROMPT}]),
            )
            span.set_attribute(ga.GEN_AI_INPUT_MESSAGES, json.dumps(input_messages))

    def on_chat_end(self, span: Span, resp: ChatResponse) -> None:
        span.set_attribute(ga.GEN_AI_RESPONSE_ID, resp.response_id)
        span.set_attribute(ga.GEN_AI_RESPONSE_MODEL, resp.model)
        span.set_attribute(ga.GEN_AI_RESPONSE_FINISH_REASONS, [resp.finish_reason])
        span.set_attribute(ga.GEN_AI_USAGE_INPUT_TOKENS, resp.input_tokens)
        span.set_attribute(ga.GEN_AI_USAGE_OUTPUT_TOKENS, resp.output_tokens)
        if self.capture_content:
            parts: list[dict] = []
            if resp.text:
                parts.append({"type": "text", "content": resp.text})
            for tc in resp.tool_calls:
                parts.append(
                    {"type": "tool_call", "id": tc.id, "name": tc.name, "arguments": tc.arguments}
                )
            span.set_attribute(
                ga.GEN_AI_OUTPUT_MESSAGES,
                json.dumps(
                    [{"role": "assistant", "parts": parts, "finish_reason": resp.finish_reason}]
                ),
            )

    def on_tool_start(self, span: Span, tool: Tool, call_id: str) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_EXECUTE_TOOL)
        span.set_attribute(ga.GEN_AI_TOOL_NAME, tool.name)
        span.set_attribute(ga.GEN_AI_TOOL_CALL_ID, call_id)
        span.set_attribute(ga.GEN_AI_TOOL_TYPE, "function")
        span.set_attribute(ga.GEN_AI_TOOL_DESCRIPTION, tool.description)
        span.set_attribute(ga.GEN_AI_AGENT_NAME, AGENT_NAME)

    def on_retrieval_start(self, span: Span, top_k: int) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_RETRIEVAL)
        span.set_attribute(ga.GEN_AI_DATA_SOURCE_ID, retrieval.DATA_SOURCE_ID)
        span.set_attribute(ga.GEN_AI_RETRIEVAL_TOP_K, top_k)

    def on_agent_start(self, span: Span, conversation_id: str | None) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_INVOKE_AGENT)
        span.set_attribute(ga.GEN_AI_AGENT_NAME, AGENT_NAME)
        span.set_attribute(ga.GEN_AI_AGENT_DESCRIPTION, AGENT_DESCRIPTION)
        span.set_attribute(ga.GEN_AI_REQUEST_MODEL, self.provider.default_model)
        if conversation_id:
            # Only when the caller supplied one: the conventions forbid
            # inventing fallback conversation ids.
            span.set_attribute(ga.GEN_AI_CONVERSATION_ID, conversation_id)

    def on_agent_end(self, span: Span, run: AgentRun) -> None:
        span.set_attribute(ga.GEN_AI_USAGE_INPUT_TOKENS, run.input_tokens)
        span.set_attribute(ga.GEN_AI_USAGE_OUTPUT_TOKENS, run.output_tokens)
        if run.finish_reasons:
            span.set_attribute(ga.GEN_AI_RESPONSE_FINISH_REASONS, run.finish_reasons)

    # ---- span context managers --------------------------------------------

    @contextmanager
    def chat(self, input_messages: list[dict]):
        started = time.perf_counter()
        call = ChatCall()
        with self.tracer.start_as_current_span(self.chat_span_name(), kind=SpanKind.CLIENT) as span:
            self.on_chat_start(span, input_messages)
            try:
                yield call
            except Exception as exc:
                call.error_type = type(exc).__name__
                span.set_attribute(ga.ERROR_TYPE, call.error_type)
                span.set_status(StatusCode.ERROR, str(exc))
                raise
            finally:
                if call.response is not None:
                    self.on_chat_end(span, call.response)
                self._record_chat_metrics(call, time.perf_counter() - started)

    @contextmanager
    def execute_tool(self, tool: Tool, call_id: str):
        started = time.perf_counter()
        with self.tracer.start_as_current_span(
            self.tool_span_name(tool.name), kind=SpanKind.INTERNAL
        ) as span:
            self.on_tool_start(span, tool, call_id)
            try:
                yield span
            finally:
                self.tool_duration.record(
                    time.perf_counter() - started,
                    {
                        ga.GEN_AI_TOOL_NAME: tool.name,
                        ga.GEN_AI_TOOL_TYPE: "function",
                        ga.GEN_AI_AGENT_NAME: AGENT_NAME,
                    },
                )

    @contextmanager
    def retrieval_span(self, top_k: int):
        with self.tracer.start_as_current_span(
            self.retrieval_span_name(), kind=SpanKind.CLIENT
        ) as span:
            self.on_retrieval_start(span, top_k)
            yield span

    @contextmanager
    def invoke_agent(self, conversation_id: str | None):
        started = time.perf_counter()
        run = AgentRun()
        with self.tracer.start_as_current_span(
            self.agent_span_name(), kind=SpanKind.INTERNAL
        ) as span:
            ctx = span.get_span_context()
            run.trace_id_hex = f"{ctx.trace_id:032x}"
            run.span_id_hex = f"{ctx.span_id:016x}"
            self.on_agent_start(span, conversation_id)
            try:
                yield run
            finally:
                self.on_agent_end(span, run)
                self.agent_duration.record(
                    time.perf_counter() - started,
                    {
                        ga.GEN_AI_AGENT_NAME: AGENT_NAME,
                        ga.GEN_AI_REQUEST_MODEL: self.provider.default_model,
                    },
                )

    # ---- metrics -----------------------------------------------------------

    def _record_chat_metrics(self, call: ChatCall, elapsed_s: float) -> None:
        base: dict[str, str] = {
            ga.GEN_AI_OPERATION_NAME: ga.OP_CHAT,
            ga.GEN_AI_PROVIDER_NAME: self.provider.name,
            ga.GEN_AI_REQUEST_MODEL: self.provider.default_model,
        }
        if call.response is not None:
            base[ga.GEN_AI_RESPONSE_MODEL] = call.response.model
        duration_attrs = dict(base)
        if call.error_type is not None:
            duration_attrs[ga.ERROR_TYPE] = call.error_type
        self.op_duration.record(elapsed_s, duration_attrs)
        if call.response is not None:
            self.token_usage.record(
                call.response.input_tokens, {**base, ga.GEN_AI_TOKEN_TYPE: "input"}
            )
            self.token_usage.record(
                call.response.output_tokens, {**base, ga.GEN_AI_TOKEN_TYPE: "output"}
            )
