"""Traffic generator: multi-turn conversations against a running demo.

Each turn starts its own CLIENT span (service.name "demo-traffic") and
propagates W3C traceparent into the request — so the gateway sees genuine
cross-service traces: traffic client span → FastAPI server span →
invoke_agent → chats/tools/retrieval.
"""

import argparse
import random
import time
import uuid

import httpx
from opentelemetry import propagate
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.trace import SpanKind

from agent_demo.evals.runner import load_scenarios


def build_tracer_provider() -> TracerProvider:
    import os

    resource = Resource.create({"service.name": "demo-traffic", "service.version": "0.1.0"})
    provider = TracerProvider(resource=resource)
    if os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT"):
        from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
        from opentelemetry.sdk.trace.export import BatchSpanProcessor

        provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    return provider


def run(endpoint: str, conversations: int, pace_s: float, loop: bool) -> None:
    provider = build_tracer_provider()
    tracer = provider.get_tracer("demo_traffic", "0.1.0")
    scenarios = load_scenarios()
    rng = random.Random(42)

    with httpx.Client(base_url=endpoint, timeout=30.0) as client:
        round_num = 0
        while True:
            for i in range(conversations):
                sc = scenarios[(round_num * conversations + i) % len(scenarios)]
                conversation_id = f"conv_{uuid.uuid4().hex[:16]}"
                # Two turns per conversation: the second reuses the
                # conversation id, exercising gen_ai.conversation.id.
                for turn, question in enumerate(
                    [sc.question, f"Thanks! One more detail about: {sc.question}"]
                ):
                    with tracer.start_as_current_span("POST /ask", kind=SpanKind.CLIENT):
                        headers: dict[str, str] = {}
                        propagate.inject(headers)
                        resp = client.post(
                            "/ask",
                            json={"question": question, "conversation_id": conversation_id},
                            headers=headers,
                        )
                        resp.raise_for_status()
                        body = resp.json()
                        print(f"conv {conversation_id} turn {turn + 1}: trace {body['trace_id']}")
                    time.sleep(rng.uniform(0.2, 1.0) * pace_s)
            round_num += 1
            if not loop:
                break
    provider.force_flush()
    provider.shutdown()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", default="http://localhost:8000")
    parser.add_argument("--conversations", type=int, default=4)
    parser.add_argument("--pace", type=float, default=1.0, help="pacing multiplier (seconds)")
    parser.add_argument(
        "--loop", action="store_true", help="run forever (compose/kind demo traffic)"
    )
    args = parser.parse_args()
    run(args.endpoint, args.conversations, args.pace, args.loop)


if __name__ == "__main__":
    main()
