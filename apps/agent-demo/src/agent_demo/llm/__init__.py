"""LLM provider abstraction: mock-first, real-provider optional.

The default provider is a deterministic mock so the whole demo runs with zero
API keys, zero cost, and byte-stable evaluation scores (ADR-0003 in the repo
docs). Setting OPENAI_API_KEY or ANTHROPIC_API_KEY swaps in a real provider
behind the same protocol — the instrumentation layer is provider-agnostic and
the emitted telemetry keeps the same shape, only gen_ai.provider.name and the
server.address/port attributes change.
"""

import os
from dataclasses import dataclass, field
from typing import Protocol


@dataclass(frozen=True)
class ToolCall:
    id: str
    name: str
    arguments: dict[str, str]


@dataclass(frozen=True)
class ChatResponse:
    text: str
    response_id: str
    model: str
    finish_reason: str  # "stop" or "tool_calls"
    input_tokens: int
    output_tokens: int
    tool_calls: list[ToolCall] = field(default_factory=list)


class Provider(Protocol):
    """A chat-completion backend."""

    name: str
    default_model: str
    # Real providers expose their API host so spans can carry
    # server.address/server.port; the in-process mock has neither.
    server_address: str | None
    server_port: int | None

    def plan(self, question: str) -> ChatResponse:
        """First model call: decide which tools to invoke."""
        ...

    def answer(
        self, question: str, tool_results: dict[str, str], context_docs: list[str]
    ) -> ChatResponse:
        """Second model call: compose the final answer."""
        ...


def from_env() -> Provider:
    """Pick the provider: real if a key is present, mock otherwise."""
    if os.environ.get("OPENAI_API_KEY"):
        from agent_demo.llm.openai import OpenAIProvider

        return OpenAIProvider()
    if os.environ.get("ANTHROPIC_API_KEY"):
        from agent_demo.llm.anthropic import AnthropicProvider

        return AnthropicProvider()
    from agent_demo.llm.mock import MockProvider

    return MockProvider()
