"""FastAPI entrypoint.

The HTTP layer is instrumented with the standard FastAPI instrumentation so
each request gets a conventional SERVER span — the root the GenAI spans hang
off. The /ask response returns the invoke_agent span's trace/span ids so
external evaluators can attach gen_ai.evaluation.result events to exactly
the operation they judged.
"""

import os

import uvicorn
from fastapi import FastAPI
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from pydantic import BaseModel

from agent_demo import llm
from agent_demo.agent import TripPlannerAgent
from agent_demo.llm import Provider
from agent_demo.noncompliant import BrokenInstrumentation
from agent_demo.telemetry import GenAIInstrumentation, TelemetryBundle, build_telemetry


class AskRequest(BaseModel):
    question: str
    conversation_id: str | None = None


class RetrievedDoc(BaseModel):
    id: str
    score: float


class AskResponse(BaseModel):
    answer: str
    response_id: str
    trace_id: str
    span_id: str
    conversation_id: str | None
    used_tools: list[str]
    retrieved: list[RetrievedDoc]


def env_flag(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in {"1", "true", "yes", "on"}


def create_app(
    bundle: TelemetryBundle | None = None,
    provider: Provider | None = None,
    capture_content: bool | None = None,
    noncompliant: bool | None = None,
) -> FastAPI:
    """Build the app; tests and the offline runner inject their own bundle."""
    if noncompliant is None:
        noncompliant = env_flag("DEMO_NONCOMPLIANT")
    if capture_content is None:
        capture_content = env_flag("DEMO_CAPTURE_CONTENT")
    if bundle is None:
        # Servers without an OTLP endpoint instrument-but-don't-export:
        # in-memory capture (the tests' and offline runner's mode) would
        # accumulate every span for the life of the process.
        mode = "otlp" if os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT") else "none"
        bundle = build_telemetry(mode=mode, noncompliant=noncompliant)
    if provider is None:
        provider = llm.from_env()

    instr_cls = BrokenInstrumentation if noncompliant else GenAIInstrumentation
    agent = TripPlannerAgent(instr_cls(bundle, provider, capture_content), provider)

    app = FastAPI(title="agent-demo", version="0.1.0")
    app.state.bundle = bundle

    @app.post("/ask")
    def ask(req: AskRequest) -> AskResponse:
        result = agent.ask(req.question, req.conversation_id)
        return AskResponse(
            answer=result.answer,
            response_id=result.response_id,
            trace_id=result.trace_id,
            span_id=result.span_id,
            conversation_id=result.conversation_id,
            used_tools=result.used_tools,
            retrieved=[
                RetrievedDoc(id=h.document.id, score=round(h.score, 4)) for h in result.retrieved
            ],
        )

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        return {"status": "ok"}

    FastAPIInstrumentor.instrument_app(
        app,
        tracer_provider=bundle.tracer_provider,
        excluded_urls="healthz",
    )
    return app


def main() -> None:
    uvicorn.run(create_app(), host="0.0.0.0", port=int(os.environ.get("PORT", "8000")))  # noqa: S104


if __name__ == "__main__":
    main()
