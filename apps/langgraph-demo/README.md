# langgraph-demo

A real [LangGraph](https://github.com/langchain-ai/langgraph) agent whose telemetry conforms to the
OpenTelemetry GenAI semantic conventions — bridged by one compact, custom
[`BaseCallbackHandler`](src/langgraph_demo/otel_callbacks.py).

The graph is a small travel helper: a retrieval node over a tiny city-facts corpus, an agent node
with two bound tools (`unit_converter`, `city_facts`), and a conditional tools loop. Each
invocation produces one trace:

```
invoke_agent langgraph-travel-helper        (INTERNAL, root)
├── retrieval lg-city-kb                    (CLIENT)
├── chat mock-travel-s1                     (CLIENT, tool_calls)
├── execute_tool unit_converter             (INTERNAL)
└── chat mock-travel-s1                     (CLIENT, stop)
```

plus `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` histograms with the
conventions' advisory bucket boundaries.

## Run it

```sh
uv sync

# Offline: write OTLP protobuf files and judge them with the Go engine
uv run python -m langgraph_demo.main --otlp-out out/run
(cd ../.. && go run ./cmd/genai-conformance validate \
    apps/langgraph-demo/out/run.traces.binpb apps/langgraph-demo/out/run.metrics.binpb)

# Live: export OTLP/HTTP (e.g. into the docker-compose stack)
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 uv run python -m langgraph_demo.main
```

By default a deterministic scripted chat model plays the two-turn tool-calling protocol — no API
keys, identical telemetry every run (`gen_ai.provider.name="mock"` shows up in the conformance
report as an INFO hint, by design). Set `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` (with the `real`
extra installed: `uv sync --extra real`) and the same code path talks to the real provider; the
telemetry keeps its shape, only the provider/model attributes change.

## Why a custom callback handler instead of openllmetry / openinference?

Those ecosystems instrument many frameworks at once and carry their own attribute dialects and
dependencies. The point of this demo is the opposite: to show that the callback surface LangChain
already exposes (`on_chat_model_start`, `on_tool_start`, ...) maps onto the GenAI semantic
conventions in ~300 lines you fully control — pinned to the exact conventions snapshot the
conformance suite enforces, with explicit span parenting that survives LangGraph's worker threads.
They solve breadth; this solves fidelity to a pinned spec.

## Tests

```sh
uv run pytest      # bridge span shapes, metric shapes, determinism, and the Go conformance gate
uv run ruff check .
```
