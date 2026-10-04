"""OpenAI-backed provider, used when OPENAI_API_KEY is set.

The SDK import is lazy so the base install never needs it (install with
`uv sync --extra real`). Telemetry shape is identical to the mock's; only
gen_ai.provider.name ("openai") and server.address/port change — that
symmetry is the point of the demo's mock-first design.
"""

import json
import os

from agent_demo.llm import ChatResponse, ToolCall

_TOOL_SCHEMAS = [
    {
        "type": "function",
        "function": {
            "name": "city_facts",
            "description": "Look up country, population, and currency for a city",
            "parameters": {
                "type": "object",
                "properties": {"city": {"type": "string"}},
                "required": ["city"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "calculator",
            "description": "Evaluate a basic arithmetic expression",
            "parameters": {
                "type": "object",
                "properties": {"expression": {"type": "string"}},
                "required": ["expression"],
            },
        },
    },
]

_SYSTEM = "You are trip-planner, a concise travel assistant. Use tools when helpful."


class OpenAIProvider:
    name = "openai"
    server_address = "api.openai.com"
    server_port = 443

    def __init__(self) -> None:
        from openai import OpenAI  # lazy: optional dependency

        self._client = OpenAI()
        self.default_model = os.environ.get("DEMO_OPENAI_MODEL", "gpt-4o-mini")

    def plan(self, question: str) -> ChatResponse:
        resp = self._client.chat.completions.create(
            model=self.default_model,
            temperature=0.2,
            max_tokens=512,
            messages=[
                {"role": "system", "content": _SYSTEM},
                {"role": "user", "content": question},
            ],
            tools=_TOOL_SCHEMAS,
        )
        choice = resp.choices[0]
        calls = [
            ToolCall(id=tc.id, name=tc.function.name, arguments=json.loads(tc.function.arguments))
            for tc in (choice.message.tool_calls or [])
        ]
        return ChatResponse(
            text=choice.message.content or "",
            response_id=resp.id,
            model=resp.model,
            finish_reason=choice.finish_reason,
            input_tokens=resp.usage.prompt_tokens if resp.usage else 0,
            output_tokens=resp.usage.completion_tokens if resp.usage else 0,
            tool_calls=calls,
        )

    def answer(
        self, question: str, tool_results: dict[str, str], context_docs: list[str]
    ) -> ChatResponse:
        context = "\n".join(
            [f"Tool {name}: {result}" for name, result in tool_results.items()]
            + [f"Note: {doc}" for doc in context_docs]
        )
        resp = self._client.chat.completions.create(
            model=self.default_model,
            temperature=0.2,
            max_tokens=512,
            messages=[
                {"role": "system", "content": _SYSTEM},
                {"role": "user", "content": f"{question}\n\nContext:\n{context}"},
            ],
        )
        choice = resp.choices[0]
        return ChatResponse(
            text=choice.message.content or "",
            response_id=resp.id,
            model=resp.model,
            finish_reason=choice.finish_reason,
            input_tokens=resp.usage.prompt_tokens if resp.usage else 0,
            output_tokens=resp.usage.completion_tokens if resp.usage else 0,
        )
