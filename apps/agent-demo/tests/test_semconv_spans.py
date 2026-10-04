"""Semconv shape tests at the SDK level: the same assertions the Go
conformance suite makes on the wire, verified here before anything is
exported. This is the demo's first line of defense against drift."""

import json

from conftest import PARIS_QUESTION, make_demo
from opentelemetry.trace import SpanKind

DEPRECATED_KEYS = {"gen_ai.system", "gen_ai.usage.prompt_tokens", "gen_ai.usage.completion_tokens"}
CONTENT_KEYS = {"gen_ai.input.messages", "gen_ai.output.messages", "gen_ai.system_instructions"}


def ask(demo, conversation_id=None):
    resp = demo.client.post(
        "/ask", json={"question": PARIS_QUESTION, "conversation_id": conversation_id}
    )
    assert resp.status_code == 200, resp.text
    return resp.json()


def test_ask_returns_span_linkage():
    demo = make_demo()
    body = ask(demo)
    assert len(body["trace_id"]) == 32
    assert len(body["span_id"]) == 16
    assert body["response_id"].startswith("chatcmpl-mock-")
    assert body["used_tools"] == ["city_facts", "calculator"]
    assert len(body["retrieved"]) == 3


def test_invoke_agent_span_shape():
    demo = make_demo()
    body = ask(demo, conversation_id="conv_test_1")
    agents = demo.genai_spans("invoke_agent")
    assert len(agents) == 1
    span = agents[0]
    attrs = dict(span.attributes)
    assert span.name == "invoke_agent trip-planner"
    # invoke_agent runs in-process → INTERNAL, and therefore no
    # gen_ai.provider.name requirement.
    assert span.kind == SpanKind.INTERNAL
    assert "gen_ai.provider.name" not in attrs
    assert attrs["gen_ai.agent.name"] == "trip-planner"
    assert attrs["gen_ai.request.model"] == "mock-small-1"
    assert attrs["gen_ai.conversation.id"] == "conv_test_1"
    assert (
        isinstance(attrs["gen_ai.usage.input_tokens"], int)
        and attrs["gen_ai.usage.input_tokens"] > 0
    )
    assert (
        isinstance(attrs["gen_ai.usage.output_tokens"], int)
        and attrs["gen_ai.usage.output_tokens"] > 0
    )
    assert tuple(attrs["gen_ai.response.finish_reasons"]) == ("stop",)
    # The API must report exactly this span for evaluation linkage.
    assert f"{span.context.span_id:016x}" == body["span_id"]
    assert f"{span.context.trace_id:032x}" == body["trace_id"]


def test_conversation_id_only_when_supplied():
    demo = make_demo()
    ask(demo, conversation_id=None)
    attrs = dict(demo.genai_spans("invoke_agent")[0].attributes)
    # The conventions forbid inventing fallback conversation ids.
    assert "gen_ai.conversation.id" not in attrs


def test_chat_span_shape():
    demo = make_demo()
    ask(demo)
    chats = demo.genai_spans("chat")
    assert len(chats) == 2  # plan + answer
    for span in chats:
        attrs = dict(span.attributes)
        assert span.name == "chat mock-small-1"
        assert span.kind == SpanKind.CLIENT
        assert attrs["gen_ai.provider.name"] == "mock"
        assert attrs["gen_ai.request.model"] == "mock-small-1"
        assert attrs["gen_ai.response.model"] == "mock-small-1"
        assert isinstance(attrs["gen_ai.request.temperature"], float)
        assert isinstance(attrs["gen_ai.request.max_tokens"], int)
        assert attrs["gen_ai.response.id"].startswith("chatcmpl-mock-")
        assert isinstance(attrs["gen_ai.usage.input_tokens"], int)
        assert isinstance(attrs["gen_ai.usage.output_tokens"], int)
        assert attrs["gen_ai.usage.output_tokens"] >= 0
        assert isinstance(attrs["gen_ai.response.finish_reasons"], tuple)
        # The mock is in-process: no server.address, and critically never a
        # server.address without server.port.
        assert "server.address" not in attrs and "server.port" not in attrs
        assert not DEPRECATED_KEYS & attrs.keys()
        assert not CONTENT_KEYS & attrs.keys()  # content capture is opt-in


def test_tool_and_retrieval_span_shapes():
    demo = make_demo()
    ask(demo)

    tools = demo.genai_spans("execute_tool")
    assert {dict(s.attributes)["gen_ai.tool.name"] for s in tools} == {"city_facts", "calculator"}
    for span in tools:
        attrs = dict(span.attributes)
        assert span.kind == SpanKind.INTERNAL
        assert span.name == f"execute_tool {attrs['gen_ai.tool.name']}"
        assert attrs["gen_ai.tool.call.id"].startswith("call_")
        assert attrs["gen_ai.tool.type"] == "function"
        assert attrs["gen_ai.agent.name"] == "trip-planner"

    retrievals = demo.genai_spans("retrieval")
    assert len(retrievals) == 1
    attrs = dict(retrievals[0].attributes)
    assert retrievals[0].name == "retrieval travel-kb"
    assert retrievals[0].kind == SpanKind.CLIENT
    assert attrs["gen_ai.data_source.id"] == "travel-kb"
    assert attrs["gen_ai.retrieval.top_k"] == 3


def test_trace_topology():
    demo = make_demo()
    ask(demo)
    spans = demo.spans()
    server = [s for s in spans if s.kind == SpanKind.SERVER]
    assert len(server) == 1, "FastAPI instrumentation must contribute the SERVER root"
    agent = demo.genai_spans("invoke_agent")[0]
    assert agent.parent is not None and agent.parent.span_id == server[0].context.span_id
    for op in ("chat", "execute_tool", "retrieval"):
        for span in demo.genai_spans(op):
            assert span.parent.span_id == agent.context.span_id, (
                f"{op} must be a child of invoke_agent"
            )


def test_content_capture_opt_in():
    demo = make_demo(capture_content=True)
    ask(demo)
    for span in demo.genai_spans("chat"):
        attrs = dict(span.attributes)
        messages = json.loads(attrs["gen_ai.input.messages"])
        assert messages[0]["role"] == "user"
        assert messages[0]["parts"][0]["type"] == "text"
        instructions = json.loads(attrs["gen_ai.system_instructions"])
        assert instructions[0]["type"] == "text"
        output = json.loads(attrs["gen_ai.output.messages"])
        assert output[0]["role"] == "assistant"
        assert "finish_reason" in output[0]


def test_metrics_shapes():
    demo = make_demo()
    ask(demo)
    data = demo.bundle.metric_reader.get_metrics_data()
    metrics = {
        m.name: m for rm in data.resource_metrics for sm in rm.scope_metrics for m in sm.metrics
    }
    expected = {
        "gen_ai.client.token.usage": "{token}",
        "gen_ai.client.operation.duration": "s",
        "gen_ai.invoke_agent.duration": "s",
        "gen_ai.execute_tool.duration": "s",
    }
    for name, unit in expected.items():
        assert name in metrics, f"missing metric {name}"
        assert metrics[name].unit == unit

    token_points = metrics["gen_ai.client.token.usage"].data.data_points
    assert {p.attributes["gen_ai.token.type"] for p in token_points} == {"input", "output"}
    for p in token_points:
        assert p.attributes["gen_ai.operation.name"] == "chat"
        assert p.attributes["gen_ai.provider.name"] == "mock"
        # Advisory bucket boundaries from the conventions docs.
        assert list(p.explicit_bounds)[:3] == [1, 4, 16]

    tool_points = metrics["gen_ai.execute_tool.duration"].data.data_points
    assert all("gen_ai.tool.name" in p.attributes for p in tool_points)
