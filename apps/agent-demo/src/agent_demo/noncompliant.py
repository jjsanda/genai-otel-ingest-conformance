"""The intentionally broken telemetry emitter — the demo's foil.

Every override below violates the pinned GenAI conventions ON PURPOSE so the
conformance gateway has something to catch red-handed. Each violation names
the rule it trips. Combined with build_telemetry(noncompliant=True), which
drops service.name/service.version from the resource (GENAI-RES-001/002),
one run of this emitter demonstrates most of the rule catalog.

Nothing here is ever active unless DEMO_NONCOMPLIANT=1 (or the eval runner's
--noncompliant flag) is set explicitly.
"""

import json

from opentelemetry.trace.span import Span

from agent_demo import genai_attrs as ga
from agent_demo.llm import ChatResponse
from agent_demo.telemetry import GenAIInstrumentation
from agent_demo.tools import Tool


class BrokenInstrumentation(GenAIInstrumentation):
    def chat_span_name(self) -> str:
        # GENAI-SPAN-003: conventions want "chat {gen_ai.request.model}".
        return "LLM call"

    def on_chat_start(self, span: Span, input_messages: list[dict]) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_CHAT)
        # GENAI-SPAN-009 + GENAI-SPAN-001: the deprecated gen_ai.system
        # instead of the required gen_ai.provider.name.
        span.set_attribute(ga.DEPRECATED_GEN_AI_SYSTEM, self.provider.name)
        span.set_attribute(ga.GEN_AI_REQUEST_MODEL, self.provider.default_model)
        # GENAI-SPAN-008: server.address without server.port.
        span.set_attribute(ga.SERVER_ADDRESS, "llm.internal")
        # GENAI-SPAN-012: prompt content in a legacy span event instead of
        # the opt-in gen_ai.input.messages attribute.
        span.add_event(
            ga.LEGACY_EVENT_CONTENT_PROMPT,
            {"gen_ai.prompt": json.dumps(input_messages)},
        )

    def on_chat_end(self, span: Span, resp: ChatResponse) -> None:
        # GENAI-SPAN-005: token counts must be ints, not strings.
        span.set_attribute(ga.GEN_AI_USAGE_INPUT_TOKENS, str(resp.input_tokens))
        # GENAI-SPAN-006: token counts are never negative.
        span.set_attribute(ga.GEN_AI_USAGE_OUTPUT_TOKENS, -resp.output_tokens)
        # GENAI-SPAN-009: deprecated token attribute name.
        span.set_attribute(ga.DEPRECATED_COMPLETION_TOKENS, resp.output_tokens)
        # GENAI-SPAN-015: an attribute no conventions version ever defined.
        span.set_attribute(ga.BOGUS_TOTAL_TOKENS, resp.input_tokens + resp.output_tokens)
        # Omitting response.model/id/finish_reasons also trips the
        # missing-recommended-attributes warning (GENAI-SPAN-010).

    def on_tool_start(self, span: Span, tool: Tool, call_id: str) -> None:
        span.set_attribute(ga.GEN_AI_OPERATION_NAME, ga.OP_EXECUTE_TOOL)
        # GENAI-SPAN-001: gen_ai.tool.name is REQUIRED on execute_tool spans
        # and deliberately dropped here.
        span.set_attribute(ga.GEN_AI_TOOL_CALL_ID, call_id)
