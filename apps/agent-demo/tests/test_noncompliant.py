"""The broken emitter must break exactly what it claims — nothing more.

Half of these assertions run at the SDK level; the end-to-end half feeds a
broken export through the Go validator and checks the precise rule IDs.
"""

import json

from conftest import make_demo
from test_offline_validate import validate

from agent_demo.evals.runner import run_offline

PARIS_QUESTION = "What is the population of Paris? Also compute 12*7 for my notes."


def test_noncompliant_mode_breaks_exactly_what_it_claims():
    demo = make_demo(noncompliant=True)
    resp = demo.client.post("/ask", json={"question": PARIS_QUESTION})
    assert resp.status_code == 200

    # Resource: SDK fallback service.name (GENAI-RES-001 material).
    resource_attrs = dict(demo.spans()[0].resource.attributes)
    assert resource_attrs["service.name"].startswith("unknown_service")
    assert "service.version" not in resource_attrs

    chats = demo.genai_spans("chat")
    assert len(chats) == 2
    for span in chats:
        attrs = dict(span.attributes)
        assert span.name == "LLM call"  # GENAI-SPAN-003
        assert "gen_ai.provider.name" not in attrs  # GENAI-SPAN-001
        assert attrs["gen_ai.system"] == "mock"  # GENAI-SPAN-009
        assert isinstance(attrs["gen_ai.usage.input_tokens"], str)  # GENAI-SPAN-005
        assert attrs["gen_ai.usage.output_tokens"] < 0  # GENAI-SPAN-006
        assert "gen_ai.usage.total_tokens" in attrs  # GENAI-SPAN-015
        assert "server.address" in attrs and "server.port" not in attrs  # GENAI-SPAN-008
        assert [e.name for e in span.events] == ["gen_ai.content.prompt"]  # GENAI-SPAN-012
        prompt_payload = json.loads(dict(span.events[0].attributes)["gen_ai.prompt"])
        assert prompt_payload[0]["role"] == "user"

    for span in demo.genai_spans("execute_tool"):
        assert "gen_ai.tool.name" not in dict(span.attributes)  # GENAI-SPAN-001


def test_noncompliant_export_is_caught(tmp_path):
    paths = run_offline(str(tmp_path / "broken"), noncompliant=True)

    code, report = validate(paths)
    assert code == 1, "broken telemetry must fail the gate"
    observed = {f["rule_id"] for f in report["findings"]}
    expected_errors = {
        "GENAI-SPAN-001",  # provider.name / tool.name missing
        "GENAI-SPAN-005",  # string token count
        "GENAI-SPAN-006",  # negative token count
        "GENAI-SPAN-008",  # server.address without server.port
        "GENAI-SPAN-009",  # deprecated gen_ai.system & friends
        "GENAI-SPAN-012",  # legacy content span event
        "GENAI-RES-001",  # unknown_service
    }
    missing = expected_errors - observed
    assert not missing, f"expected rules not triggered: {missing}; observed: {sorted(observed)}"
    assert report["summary"]["errors"] > 0
