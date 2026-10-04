"""Evaluation runner: exercise the agent, judge the answers, and emit
gen_ai.evaluation.result events tied to the traces they judge.

Offline mode (--otlp-out PREFIX) is the CI path: the FastAPI app runs
in-process behind httpx's ASGI transport (zero network), all telemetry is
captured in memory, and everything — app spans, app metrics, and the
runner's evaluation events — lands in three .binpb files that
`genai-conformance validate` gates on.

Endpoint mode (--endpoint URL) drives a running deployment instead and
ships evaluation events via OTLP when OTEL_EXPORTER_OTLP_ENDPOINT is set.
"""

import argparse
import asyncio
import importlib.resources
import json
import sys
from dataclasses import dataclass
from pathlib import Path

import httpx
from opentelemetry.sdk.resources import Resource

from agent_demo import genai_attrs as ga
from agent_demo import otel_compat
from agent_demo.evals.judges import (
    Judgment,
    judge_correctness,
    judge_groundedness,
    judge_tool_precision,
)
from agent_demo.otlp_file import export_files
from agent_demo.retrieval import VectorStore
from agent_demo.telemetry import build_telemetry

EVAL_SERVICE_NAME = "demo-eval-runner"
EVAL_SERVICE_VERSION = "0.1.0"


@dataclass(frozen=True)
class Scenario:
    id: str
    question: str
    expected_answer_fragment: str
    expected_tools: list[str]
    grounded_in: list[str]


@dataclass(frozen=True)
class ScenarioRun:
    scenario: Scenario
    answer: str
    response_id: str
    trace_id: str
    span_id: str
    used_tools: list[str]
    retrieved_ids: list[str]
    judgments: list[Judgment]


def load_scenarios() -> list[Scenario]:
    raw = importlib.resources.files("agent_demo.evals").joinpath("scenarios.json").read_text()
    return [Scenario(**item) for item in json.loads(raw)]


async def _run_scenarios(client: httpx.AsyncClient, scenarios: list[Scenario]) -> list[ScenarioRun]:
    store = VectorStore()  # for resolving retrieved doc texts when judging
    runs: list[ScenarioRun] = []
    for sc in scenarios:
        resp = await client.post("/ask", json={"question": sc.question})
        resp.raise_for_status()
        body = resp.json()
        retrieved_ids = [d["id"] for d in body["retrieved"]]
        retrieved_texts = [doc.text for doc_id in retrieved_ids if (doc := store.by_id(doc_id))]
        judgments = [
            judge_correctness(body["answer"], sc.expected_answer_fragment),
            judge_groundedness(body["answer"], retrieved_texts),
            judge_tool_precision(body["used_tools"], sc.expected_tools),
        ]
        runs.append(
            ScenarioRun(
                scenario=sc,
                answer=body["answer"],
                response_id=body["response_id"],
                trace_id=body["trace_id"],
                span_id=body["span_id"],
                used_tools=body["used_tools"],
                retrieved_ids=retrieved_ids,
                judgments=judgments,
            )
        )
    return runs


def _emit_events(provider: otel_compat.LoggerProvider, runs: list[ScenarioRun]) -> int:
    """One gen_ai.evaluation.result event per judge per scenario, linked to
    the invoke_agent span the API reported."""
    count = 0
    for run in runs:
        for j in run.judgments:
            otel_compat.emit_event(
                provider,
                event_name=ga.EVENT_EVALUATION_RESULT,
                attributes={
                    ga.GEN_AI_EVALUATION_NAME: j.name,
                    ga.GEN_AI_EVALUATION_SCORE_VALUE: float(j.score),
                    ga.GEN_AI_EVALUATION_SCORE_LABEL: j.label,
                    ga.GEN_AI_EVALUATION_EXPLANATION: j.explanation,
                    ga.GEN_AI_RESPONSE_ID: run.response_id,
                },
                trace_id_hex=run.trace_id,
                span_id_hex=run.span_id,
            )
            count += 1
    return count


def _summarize(runs: list[ScenarioRun], out=sys.stdout) -> None:
    by_judge: dict[str, list[float]] = {}
    for run in runs:
        for j in run.judgments:
            by_judge.setdefault(j.name, []).append(j.score)
    print(f"\nEvaluated {len(runs)} scenarios:", file=out)
    for name, scores in sorted(by_judge.items()):
        mean = sum(scores) / len(scores)
        passing = sum(1 for s in scores if s >= 0.5)
        print(f"  {name:<20} mean {mean:0.3f}   {passing}/{len(scores)} scenarios ≥ 0.5", file=out)


def run_offline(prefix: str, noncompliant: bool = False) -> list[Path]:
    """The CI path: in-process app, in-memory capture, .binpb export."""
    from agent_demo.main import create_app  # late import to keep CLI startup light

    bundle = build_telemetry(mode="memory", noncompliant=noncompliant)
    app = create_app(bundle=bundle, noncompliant=noncompliant)

    eval_resource = Resource.create(
        {"service.name": EVAL_SERVICE_NAME, "service.version": EVAL_SERVICE_VERSION}
    )
    log_exporter = otel_compat.InMemoryLogExporter()
    logger_provider = otel_compat.new_logger_provider(eval_resource, log_exporter)

    async def drive() -> list[ScenarioRun]:
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(
            transport=transport, base_url="http://agent-demo.local"
        ) as client:
            return await _run_scenarios(client, load_scenarios())

    runs = asyncio.run(drive())
    emitted = _emit_events(logger_provider, runs)
    logger_provider.force_flush()

    paths = export_files(prefix, bundle, log_exporter)
    _summarize(runs)
    print(f"\nEmitted {emitted} gen_ai.evaluation.result events; wrote:")
    for p in paths:
        print(f"  {p}")
    return paths


def run_against_endpoint(endpoint: str) -> None:
    """Judge a live deployment; events go to OTLP if configured."""
    import os

    async def drive() -> list[ScenarioRun]:
        async with httpx.AsyncClient(base_url=endpoint, timeout=30.0) as client:
            return await _run_scenarios(client, load_scenarios())

    runs = asyncio.run(drive())

    if os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"):
        from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
        from opentelemetry.sdk._logs.export import BatchLogRecordProcessor

        eval_resource = Resource.create(
            {"service.name": EVAL_SERVICE_NAME, "service.version": EVAL_SERVICE_VERSION}
        )
        provider = otel_compat.LoggerProvider(resource=eval_resource)
        provider.add_log_record_processor(BatchLogRecordProcessor(OTLPLogExporter()))
        _emit_events(provider, runs)
        provider.force_flush()
        provider.shutdown()
    _summarize(runs)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="agent_demo.evals.runner", description=__doc__)
    parser.add_argument(
        "--otlp-out",
        metavar="PREFIX",
        help="offline mode: write PREFIX.{traces,metrics,logs}.binpb",
    )
    parser.add_argument("--endpoint", metavar="URL", help="judge a running deployment instead")
    parser.add_argument(
        "--noncompliant",
        action="store_true",
        help="offline mode only: emit deliberately broken telemetry",
    )
    args = parser.parse_args(argv)

    if args.otlp_out:
        run_offline(args.otlp_out, noncompliant=args.noncompliant)
        return 0
    if args.endpoint:
        run_against_endpoint(args.endpoint)
        return 0
    parser.error("one of --otlp-out or --endpoint is required")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
