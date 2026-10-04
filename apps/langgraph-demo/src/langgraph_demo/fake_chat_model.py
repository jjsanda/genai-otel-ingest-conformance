"""A scripted chat model, plus the provider switch.

The demo must run for anyone with zero API keys and produce identical
telemetry every time (ADR-0003: mock-first). ``ScriptedChatModel`` plays a
two-turn agent protocol deterministically:

1. First call (no tool results in the conversation yet): return an
   ``AIMessage`` carrying a tool call chosen from the question's keywords.
2. Second call (tool results present): return a final answer that quotes the
   tool output.

It fills ``usage_metadata`` and ``response_metadata`` the way real LangChain
provider integrations do, because that is exactly what the telemetry bridge
reads — the bridge cannot tell it is talking to a script, which is the
point: swap in ``ChatOpenAI`` via ``OPENAI_API_KEY`` and the telemetry keeps
its shape, only ``gen_ai.provider.name`` and the model names change.
"""

from __future__ import annotations

import os
import re
from typing import Any

from langchain_core.callbacks import CallbackManagerForLLMRun
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, HumanMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from pydantic import PrivateAttr

MOCK_MODEL_NAME = "mock-travel-s1"


class ScriptedChatModel(BaseChatModel):
    """Deterministic stand-in for a chat model with tool calling."""

    model_name: str = MOCK_MODEL_NAME

    _sequence: int = PrivateAttr(default=0)

    @property
    def _llm_type(self) -> str:
        return "scripted-chat"

    def bind_tools(self, tools: Any, **kwargs: Any) -> ScriptedChatModel:
        # The script decides which tool to call from the question itself, so
        # the bound schemas are not needed; accepting the call keeps the
        # graph code identical for scripted and real models.
        return self

    def _generate(
        self,
        messages: list[BaseMessage],
        stop: list[str] | None = None,
        run_manager: CallbackManagerForLLMRun | None = None,
        **kwargs: Any,
    ) -> ChatResult:
        self._sequence += 1
        response_id = f"chatcmpl-lg-{self._sequence:04d}"
        question = next(
            (str(m.content) for m in messages if isinstance(m, HumanMessage)),
            "",
        )
        tool_results = [str(m.content) for m in messages if isinstance(m, ToolMessage)]
        # Plausible token accounting: ~4 characters per token, floor of 1.
        input_tokens = max(1, sum(len(str(m.content)) for m in messages) // 4)

        if not tool_results:
            name, args = _pick_tool(question)
            message = AIMessage(
                content="",
                tool_calls=[{"name": name, "args": args, "id": f"call_lg{self._sequence:04d}"}],
                usage_metadata={
                    "input_tokens": input_tokens,
                    "output_tokens": 19,
                    "total_tokens": input_tokens + 19,
                },
                response_metadata={
                    "model_name": self.model_name,
                    "finish_reason": "tool_calls",
                    "id": response_id,
                },
            )
        else:
            answer = f"Here is what I found: {' '.join(tool_results)}"
            output_tokens = max(1, len(answer) // 4)
            message = AIMessage(
                content=answer,
                usage_metadata={
                    "input_tokens": input_tokens,
                    "output_tokens": output_tokens,
                    "total_tokens": input_tokens + output_tokens,
                },
                response_metadata={
                    "model_name": self.model_name,
                    "finish_reason": "stop",
                    "id": response_id,
                },
            )
        return ChatResult(generations=[ChatGeneration(message=message)])


def _pick_tool(question: str) -> tuple[str, dict[str, Any]]:
    """Choose a tool call from question keywords, deterministically."""
    lowered = question.lower()
    if any(k in lowered for k in ("km", "mile", "°c", "°f", "convert")):
        match = re.search(r"(\d+(?:\.\d+)?)", question)
        value = float(match.group(1)) if match else 10.0
        unit = "km" if "km" in lowered else ("°c" if "°c" in lowered else "miles")
        return "unit_converter", {"value": value, "unit": unit}
    match = re.search(r"\b(Vienna|Prague|Paris|Tokyo|Lisbon|Oslo|Kyoto|Porto)\b", question)
    city = match.group(1) if match else "Vienna"
    return "city_facts", {"city": city}


def make_chat_model() -> tuple[BaseChatModel, str, str, str | None]:
    """Return (model, provider name, request model, server address).

    Real providers are opt-in via API keys; the imports stay lazy so the
    base install never needs their SDKs.
    """
    if os.environ.get("OPENAI_API_KEY"):
        from langchain_openai import ChatOpenAI

        model_id = os.environ.get("DEMO_OPENAI_MODEL", "gpt-4o-mini")
        return ChatOpenAI(model=model_id, temperature=0), "openai", model_id, "api.openai.com"
    if os.environ.get("ANTHROPIC_API_KEY"):
        from langchain_anthropic import ChatAnthropic

        model_id = os.environ.get("DEMO_ANTHROPIC_MODEL", "claude-haiku-4-5")
        return (
            ChatAnthropic(model=model_id, temperature=0),
            "anthropic",
            model_id,
            "api.anthropic.com",
        )
    return ScriptedChatModel(), "mock", MOCK_MODEL_NAME, None
