"""Test fixtures: an in-process demo app with in-memory telemetry capture."""

from dataclasses import dataclass

from fastapi.testclient import TestClient
from opentelemetry.sdk.trace import ReadableSpan

from agent_demo.llm.mock import MockProvider
from agent_demo.main import create_app
from agent_demo.telemetry import TelemetryBundle, build_telemetry


@dataclass
class Demo:
    client: TestClient
    bundle: TelemetryBundle

    def spans(self) -> list[ReadableSpan]:
        return list(self.bundle.span_exporter.get_finished_spans())

    def genai_spans(self, operation: str) -> list[ReadableSpan]:
        return [
            s
            for s in self.spans()
            if (s.attributes or {}).get("gen_ai.operation.name") == operation
            or (operation == "chat" and s.name == "LLM call")  # the broken emitter's chat spans
        ]


def make_demo(noncompliant: bool = False, capture_content: bool = False) -> Demo:
    bundle = build_telemetry(mode="memory", noncompliant=noncompliant)
    app = create_app(
        bundle=bundle,
        provider=MockProvider(),
        capture_content=capture_content,
        noncompliant=noncompliant,
    )
    return Demo(TestClient(app), bundle)


PARIS_QUESTION = "What is the population of Paris? Also compute 12*7 for my notes."
