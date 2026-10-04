# ADR-0003: Demo agents are mock-first, real-provider-optional

Status: accepted · Date: 2026-07-02

## Context

The demo applications exist to generate realistic GenAI telemetry. They
could call real LLM APIs (realistic but non-deterministic, paid, and
unusable in CI or by anyone without keys) or a deterministic local
mock (free and reproducible, but it must not devolve into a toy that
emits fake-looking telemetry).

## Decision

Both demo apps default to a **deterministic, scripted mock provider**:
seeded responses, scripted tool calls, and explicit token accounting, so
every run — locally, in CI, in the kind cluster — produces identical
telemetry and identical evaluation scores. Golden-file assertions become
possible end to end.

Setting `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` switches the *same code
paths* to the real provider; the instrumentation layer is provider-agnostic
and emits identical telemetry shapes either way (`gen_ai.provider.name`
changes value, not shape).

## Consequences

- `docker compose up` works for anyone with zero configuration and
  zero cost; CI asserts exact conformance scores and finding sets.
- Real-provider mode is exercised manually, not in CI; it exists to prove
  the instrumentation is not mock-shaped, and its telemetry passes the same
  conformance rules.
- The mock's latency and token numbers are synthetic but plausible
  (seeded jitter), so duration/token histograms and the Grafana dashboard
  look realistic rather than degenerate.
