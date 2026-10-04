"""The bridge's contract: LangGraph runs come out as GenAI-semconv telemetry."""

from opentelemetry.trace import SpanKind

from langgraph_demo.fake_chat_model import ScriptedChatModel
from langgraph_demo.graph import build_graph
from langgraph_demo.main import run_question
from langgraph_demo.otlp_export import setup_telemetry


def run_once(question: str):
    telemetry = setup_telemetry(memory=True)
    graph = build_graph(ScriptedChatModel())
    answer = run_question(telemetry, graph, ("mock", "mock-travel-s1", None), question)
    telemetry.tracer_provider.force_flush()
    spans = telemetry.span_exporter.get_finished_spans()
    metrics = telemetry.metric_reader.get_metrics_data()
    telemetry.shutdown()
    return answer, spans, metrics


def by_name(spans, prefix: str):
    return [s for s in spans if s.name.startswith(prefix)]


def test_trace_shape_matches_conventions():
    _, spans, _ = run_once("How far is 42 km in miles?")

    roots = [s for s in spans if s.parent is None]
    assert len(roots) == 1, "one agent invocation = one trace with one root"
    agent = roots[0]
    assert agent.name == "invoke_agent langgraph-travel-helper"
    assert agent.kind is SpanKind.INTERNAL  # in-process agent: no provider required

    trace_ids = {s.context.trace_id for s in spans}
    assert trace_ids == {agent.context.trace_id}, "every span joins the agent's trace"

    (retrieval,) = by_name(spans, "retrieval ")
    assert retrieval.kind is SpanKind.CLIENT
    assert retrieval.attributes["gen_ai.operation.name"] == "retrieval"
    assert retrieval.attributes["gen_ai.data_source.id"] == "lg-city-kb"
    assert isinstance(retrieval.attributes["gen_ai.retrieval.top_k"], int)
    assert retrieval.parent.span_id == agent.context.span_id

    chats = by_name(spans, "chat ")
    assert len(chats) == 2, "planning call + final answer call"
    for chat in chats:
        assert chat.kind is SpanKind.CLIENT
        assert chat.attributes["gen_ai.provider.name"] == "mock"
        assert chat.attributes["gen_ai.request.model"] == "mock-travel-s1"
        assert isinstance(chat.attributes["gen_ai.usage.input_tokens"], int)
        assert isinstance(chat.attributes["gen_ai.usage.output_tokens"], int)
        assert tuple(chat.attributes["gen_ai.response.finish_reasons"])
        # The mock never sets server.address; setting it without server.port
        # would violate GENAI-SPAN-008.
        assert "server.address" not in chat.attributes

    (tool,) = by_name(spans, "execute_tool ")
    assert tool.name == "execute_tool unit_converter"
    assert tool.kind is SpanKind.INTERNAL
    assert tool.attributes["gen_ai.tool.name"] == "unit_converter"
    assert tool.attributes["gen_ai.tool.type"] == "function"
    assert tool.attributes["gen_ai.tool.call.id"]
    assert tool.parent.span_id == agent.context.span_id, "tool descends from the agent"

    total_in = sum(c.attributes["gen_ai.usage.input_tokens"] for c in chats)
    total_out = sum(c.attributes["gen_ai.usage.output_tokens"] for c in chats)
    assert agent.attributes["gen_ai.usage.input_tokens"] == total_in
    assert agent.attributes["gen_ai.usage.output_tokens"] == total_out


def test_token_usage_metric_shape():
    _, _, metrics = run_once("Tell me a fact about Vienna.")
    points = []
    for rm in metrics.resource_metrics:
        for sm in rm.scope_metrics:
            for metric in sm.metrics:
                if metric.name == "gen_ai.client.token.usage":
                    assert metric.unit == "{token}"
                    points.extend(metric.data.data_points)
    assert points, "token usage histogram must exist"
    token_types = {p.attributes["gen_ai.token.type"] for p in points}
    assert token_types == {"input", "output"}
    for p in points:
        # gen_ai.token.type, provider, and operation are required on every
        # data point (conformance rule GENAI-METRIC-002).
        assert p.attributes["gen_ai.provider.name"] == "mock"
        assert p.attributes["gen_ai.operation.name"] == "chat"


def test_deterministic_runs():
    first_answer, first_spans, _ = run_once("Convert 21 °C for me.")
    second_answer, second_spans, _ = run_once("Convert 21 °C for me.")
    assert first_answer == second_answer
    assert [s.name for s in first_spans] == [s.name for s in second_spans]
    usage = [
        s.attributes.get("gen_ai.usage.input_tokens")
        for s in first_spans
        if s.name.startswith("chat ")
    ]
    usage2 = [
        s.attributes.get("gen_ai.usage.input_tokens")
        for s in second_spans
        if s.name.startswith("chat ")
    ]
    assert usage == usage2
