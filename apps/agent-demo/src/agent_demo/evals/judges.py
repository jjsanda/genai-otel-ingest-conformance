"""Three deterministic judges.

Real evaluation stacks mix heuristics with LLM-as-judge; the mock-first demo
keeps judges heuristic so scores are exactly reproducible — the judged
telemetry shape (gen_ai.evaluation.result events) is identical either way,
which is what the conformance suite cares about.
"""

import re
from dataclasses import dataclass

_WORD_RE = re.compile(r"[a-z0-9]+")


@dataclass(frozen=True)
class Judgment:
    name: str
    score: float
    label: str
    explanation: str


def judge_correctness(answer: str, expected_fragment: str) -> Judgment:
    """Binary check: does the answer contain the expected fragment?"""
    hit = expected_fragment.lower() in answer.lower()
    return Judgment(
        name="answer_correctness",
        score=1.0 if hit else 0.0,
        label="pass" if hit else "fail",
        explanation=(
            f"expected fragment {expected_fragment!r} "
            + ("found in answer" if hit else "missing from answer")
        ),
    )


def judge_groundedness(answer: str, retrieved_texts: list[str]) -> Judgment:
    """Share of answer tokens that appear in the retrieved documents.

    A crude but honest proxy for "did the answer come from the sources":
    quoting the corpus scores high, hallucinating scores low.
    """
    answer_tokens = [t for t in _WORD_RE.findall(answer.lower()) if len(t) > 3]
    if not answer_tokens:
        return Judgment("groundedness", 0.0, "ungrounded", "empty answer")
    source_tokens = set()
    for text in retrieved_texts:
        source_tokens.update(_WORD_RE.findall(text.lower()))
    overlap = sum(1 for t in answer_tokens if t in source_tokens)
    score = round(overlap / len(answer_tokens), 4)
    grounded = score >= 0.35
    return Judgment(
        name="groundedness",
        score=score,
        label="grounded" if grounded else "ungrounded",
        explanation=f"{overlap}/{len(answer_tokens)} answer tokens found in retrieved documents",
    )


def judge_tool_precision(used: list[str], expected: list[str]) -> Judgment:
    """Precision of the agent's tool choices against the scenario's plan."""
    if not expected and not used:
        return Judgment("tool_precision", 1.0, "correct", "no tools expected, none used")
    if not used:
        return Judgment("tool_precision", 0.0, "incorrect", f"expected {expected}, used none")
    correct = len(set(used) & set(expected))
    score = round(correct / len(used), 4)
    return Judgment(
        name="tool_precision",
        score=score,
        label="correct" if score == 1.0 and set(expected) <= set(used) else "incorrect",
        explanation=f"{correct} of {len(used)} tool calls were expected ({sorted(set(expected))})",
    )
