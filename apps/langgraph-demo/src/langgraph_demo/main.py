"""Run the LangGraph travel helper and emit GenAI-semconv telemetry.

Usage:
    python -m langgraph_demo.main                  # live OTLP if configured
    python -m langgraph_demo.main --otlp-out out/run   # write .binpb files
"""

from __future__ import annotations

import argparse
import sys

from opentelemetry.context import Context
from opentelemetry.trace import SpanKind, StatusCode, set_span_in_context

from langgraph_demo import otel_callbacks as attrs
from langgraph_demo.fake_chat_model import make_chat_model
from langgraph_demo.graph import AGENT_NAME, DATA_SOURCE_ID, RETRIEVAL_TOP_K, build_graph
from langgraph_demo.otel_callbacks import GenAISpanBridge
from langgraph_demo.otlp_export import Telemetry, live_mode_requested, setup_telemetry

QUESTIONS = [
    "How far is 42 km in miles?",
    "Tell me a fact about Vienna.",
    "Convert 21 °C for me.",
    "What should I know about Prague?",
]


def run_question(telemetry: Telemetry, graph, model_info, question: str) -> str:
    """One agent invocation = one trace, rooted in an invoke_agent span."""
    provider, request_model, server_address = model_info
    bridge = GenAISpanBridge(
        tracer=telemetry.tracer,
        token_usage=telemetry.token_usage,
        operation_duration=telemetry.operation_duration,
        provider=provider,
        request_model=request_model,
        agent_name=AGENT_NAME,
        data_source_id=DATA_SOURCE_ID,
        retrieval_top_k=RETRIEVAL_TOP_K,
        server_address=server_address,
    )
    # An explicit empty Context makes this span a root regardless of ambient
    # state — the request entry point of the trace (GENAI-TRACE-003).
    span = telemetry.tracer.start_span(
        f"invoke_agent {AGENT_NAME}", context=Context(), kind=SpanKind.INTERNAL
    )
    # invoke_agent runs in-process (INTERNAL), so no provider.name is
    # required here; the model attribute is meaningful because this agent is
    # configured with exactly one model.
    span.set_attribute(attrs.OPERATION_NAME, "invoke_agent")
    span.set_attribute(attrs.AGENT_NAME, AGENT_NAME)
    span.set_attribute(attrs.REQUEST_MODEL, request_model)
    bridge.root_context = set_span_in_context(span, Context())
    try:
        result = graph.invoke(
            {"question": question, "docs": [], "messages": []},
            config={"callbacks": [bridge]},
        )
        answer = str(result["messages"][-1].content)
        return answer
    except Exception as exc:
        # error.type is conditionally required when the operation fails —
        # the suite's own GENAI-SPAN-007 would flag this span without it.
        span.set_attribute(attrs.ERROR_TYPE, type(exc).__name__)
        span.set_status(StatusCode.ERROR, str(exc))
        raise
    finally:
        span.set_attribute(attrs.INPUT_TOKENS, bridge.total_input_tokens)
        span.set_attribute(attrs.OUTPUT_TOKENS, bridge.total_output_tokens)
        if bridge.finish_reasons:
            span.set_attribute(attrs.FINISH_REASONS, [bridge.finish_reasons[-1]])
        span.end()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--otlp-out",
        metavar="PREFIX",
        help="write <PREFIX>.traces.binpb and <PREFIX>.metrics.binpb instead of exporting live",
    )
    args = parser.parse_args(argv)

    memory = bool(args.otlp_out) or not live_mode_requested()
    telemetry = setup_telemetry(memory=memory)
    model, provider, request_model, server_address = make_chat_model()
    graph = build_graph(model)

    for question in QUESTIONS:
        answer = run_question(telemetry, graph, (provider, request_model, server_address), question)
        print(f"Q: {question}\nA: {answer}\n")

    if args.otlp_out:
        for path in telemetry.export_files(args.otlp_out):
            print(f"wrote {path}")
    telemetry.shutdown()
    return 0


if __name__ == "__main__":
    sys.exit(main())
