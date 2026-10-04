# agent-demo

A hand-instrumented GenAI agent (plan → tools → retrieval → answer) that
emits OpenTelemetry telemetry per the pinned [GenAI semantic
conventions](../../third_party/semconv-genai/PINNED_SHA) — the "known good"
application the conformance suite validates. It ships with a deterministic
mock LLM (zero API keys), an evaluation harness that emits
`gen_ai.evaluation.result` events, and a deliberately broken telemetry mode
for the gateway to catch.

## Run the API server

```bash
uv sync
uv run python -m agent_demo.main               # http://localhost:8000
curl -s localhost:8000/ask -X POST -H 'content-type: application/json' \
  -d '{"question": "Plan a day in Tokyo for me."}' | jq
```

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to ship telemetry (OTLP/HTTP); without it
the server runs with in-memory telemetry only.

## Offline evaluation + conformance gate (the CI path)

```bash
uv run python -m agent_demo.evals.runner --otlp-out out/run
# then, from the repo root:
go run ./cmd/genai-conformance validate apps/agent-demo/out/run.*.binpb
```

Expected: score **100%**, zero errors/warnings (INFO hints about the custom
provider name `mock` are by design).

## The money shot: broken telemetry

```bash
uv run python -m agent_demo.evals.runner --otlp-out out/broken --noncompliant
go run ./cmd/genai-conformance validate apps/agent-demo/out/broken.*.binpb
```

Every violation in [`noncompliant.py`](src/agent_demo/noncompliant.py) names
the rule it trips (deprecated `gen_ai.system`, string/negative token counts,
legacy content events, missing `service.name`, ...).

## Real providers

The same instrumentation runs against live APIs — telemetry shape is
identical, only `gen_ai.provider.name` and `server.address/port` change:

```bash
uv sync --extra real
OPENAI_API_KEY=... uv run python -m agent_demo.main        # or ANTHROPIC_API_KEY
```

## Load / demo traffic

```bash
uv run python scripts/traffic.py --endpoint http://localhost:8000 --loop
```

Two-turn conversations with W3C `traceparent` propagation from a separate
`demo-traffic` service, exercising `gen_ai.conversation.id` and
cross-service trace continuity.
