"""Deterministic mock chat provider.

Every response is a pure function of the question (plus a stable per-call
stage), with token accounting derived from text lengths and latency drawn
from a PRNG seeded by the question — plausible-looking telemetry, exactly
reproducible. gen_ai.provider.name is honestly "mock": a custom provider
value is legal under the conventions (the conformance suite surfaces it as
an INFO hint, never a violation).
"""

import hashlib
import random
import re
import time

from agent_demo.llm import ChatResponse, ToolCall
from agent_demo.tools import CITY_FACTS

MODEL = "mock-small-1"

# An arithmetic expression: digits joined by at least one operator.
_MATH_RE = re.compile(r"\(?\d[\d\s\.\)\(]*[+\-*/][\d\s\.\+\-*/\)\(]*\d\)?")

_SYSTEM_PROMPT = (
    "You are trip-planner, a travel assistant. Use the calculator and "
    "city_facts tools when they help, ground answers in retrieved notes, "
    "and keep answers short."
)


def _stable_id(*parts: str) -> str:
    digest = hashlib.sha256("|".join(parts).encode()).hexdigest()[:12]
    return f"chatcmpl-mock-{digest}"


def _tokens(text: str) -> int:
    # The classic ~4 chars/token rule of thumb: good enough for telemetry
    # that needs to look real without a tokenizer dependency.
    return max(1, len(text) // 4)


def _think(question: str, stage: str) -> None:
    # Seeded latency keeps duration histograms non-degenerate while staying
    # reproducible in spirit; values are a few milliseconds so tests fly.
    rng = random.Random(hashlib.sha256(f"{question}|{stage}".encode()).digest())
    time.sleep(rng.uniform(0.003, 0.015))


def plan_tool_calls(question: str) -> list[ToolCall]:
    """Deterministically derive tool calls from the question text."""
    calls: list[ToolCall] = []
    lowered = question.lower()
    for city in CITY_FACTS:
        if city in lowered:
            calls.append(
                ToolCall(
                    id=f"call_{_stable_id(question, city)[-8:]}",
                    name="city_facts",
                    arguments={"city": city},
                )
            )
            break  # one city per question keeps answers focused
    if match := _MATH_RE.search(question):
        expression = match.group(0).strip()
        calls.append(
            ToolCall(
                id=f"call_{_stable_id(question, 'calc')[-8:]}",
                name="calculator",
                arguments={"expression": expression},
            )
        )
    return calls


class MockProvider:
    name = "mock"
    default_model = MODEL
    server_address: str | None = None  # in-process: no server.address/port on spans
    server_port: int | None = None

    def plan(self, question: str) -> ChatResponse:
        _think(question, "plan")
        calls = plan_tool_calls(question)
        text = "" if calls else "No tools needed; answering directly."
        prompt = f"{_SYSTEM_PROMPT}\n{question}"
        return ChatResponse(
            text=text,
            response_id=_stable_id(question, "plan"),
            model=MODEL,
            finish_reason="tool_calls" if calls else "stop",
            input_tokens=_tokens(prompt),
            output_tokens=_tokens(text) if text else len(calls) * 12,
            tool_calls=calls,
        )

    def answer(
        self, question: str, tool_results: dict[str, str], context_docs: list[str]
    ) -> ChatResponse:
        _think(question, "answer")
        subject = _subject_of(question)
        parts = [f"Planning notes for {subject}:"]
        if "city_facts" in tool_results:
            parts.append(f"facts: {tool_results['city_facts']}.")
        if "calculator" in tool_results:
            parts.append(f"The math works out to {tool_results['calculator']}.")
        if context_docs:
            # Quoting the top retrieved note verbatim keeps the groundedness
            # judge honest: overlap is real, not coincidental.
            parts.append(f"From the travel notes: {context_docs[0]}")
        text = " ".join(parts)
        prompt = f"{_SYSTEM_PROMPT}\n{question}\n{tool_results}\n{' '.join(context_docs)}"
        return ChatResponse(
            text=text,
            response_id=_stable_id(question, "answer"),
            model=MODEL,
            finish_reason="stop",
            input_tokens=_tokens(prompt),
            output_tokens=_tokens(text),
        )


def _subject_of(question: str) -> str:
    lowered = question.lower()
    for city in CITY_FACTS:
        if city in lowered:
            return city.title()
    return "your trip"
