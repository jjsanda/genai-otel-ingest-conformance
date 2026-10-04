"""Anthropic-backed provider, used when ANTHROPIC_API_KEY is set.

Lazy SDK import; see llm/openai.py for the mock-first rationale. Note the
usage accounting: per the GenAI conventions, gen_ai.usage.input_tokens is
the TOTAL input including cache reads/writes, which Anthropic reports
separately from input_tokens.
"""

import os

from agent_demo.llm import ChatResponse, ToolCall

_TOOL_SCHEMAS = [
    {
        "name": "city_facts",
        "description": "Look up country, population, and currency for a city",
        "input_schema": {
            "type": "object",
            "properties": {"city": {"type": "string"}},
            "required": ["city"],
        },
    },
    {
        "name": "calculator",
        "description": "Evaluate a basic arithmetic expression",
        "input_schema": {
            "type": "object",
            "properties": {"expression": {"type": "string"}},
            "required": ["expression"],
        },
    },
]

_SYSTEM = "You are trip-planner, a concise travel assistant. Use tools when helpful."


def _total_input_tokens(usage) -> int:
    # The conventions want the total; Anthropic splits cache tokens out.
    return (
        usage.input_tokens
        + getattr(usage, "cache_read_input_tokens", 0)
        + getattr(usage, "cache_creation_input_tokens", 0)
    )


class AnthropicProvider:
    name = "anthropic"
    server_address = "api.anthropic.com"
    server_port = 443

    def __init__(self) -> None:
        from anthropic import Anthropic  # lazy: optional dependency

        self._client = Anthropic()
        self.default_model = os.environ.get("DEMO_ANTHROPIC_MODEL", "claude-haiku-4-5")

    def plan(self, question: str) -> ChatResponse:
        resp = self._client.messages.create(
            model=self.default_model,
            max_tokens=512,
            temperature=0.2,
            system=_SYSTEM,
            messages=[{"role": "user", "content": question}],
            tools=_TOOL_SCHEMAS,
        )
        calls = [
            ToolCall(id=block.id, name=block.name, arguments=dict(block.input))
            for block in resp.content
            if block.type == "tool_use"
        ]
        text = "".join(block.text for block in resp.content if block.type == "text")
        return ChatResponse(
            text=text,
            response_id=resp.id,
            model=resp.model,
            finish_reason="tool_calls" if calls else (resp.stop_reason or "stop"),
            input_tokens=_total_input_tokens(resp.usage),
            output_tokens=resp.usage.output_tokens,
            tool_calls=calls,
        )

    def answer(
        self, question: str, tool_results: dict[str, str], context_docs: list[str]
    ) -> ChatResponse:
        context = "\n".join(
            [f"Tool {name}: {result}" for name, result in tool_results.items()]
            + [f"Note: {doc}" for doc in context_docs]
        )
        resp = self._client.messages.create(
            model=self.default_model,
            max_tokens=512,
            temperature=0.2,
            system=_SYSTEM,
            messages=[{"role": "user", "content": f"{question}\n\nContext:\n{context}"}],
        )
        text = "".join(block.text for block in resp.content if block.type == "text")
        return ChatResponse(
            text=text,
            response_id=resp.id,
            model=resp.model,
            finish_reason=resp.stop_reason or "stop",
            input_tokens=_total_input_tokens(resp.usage),
            output_tokens=resp.usage.output_tokens,
        )
