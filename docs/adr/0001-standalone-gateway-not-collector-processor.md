# ADR-0001: Ship a standalone gateway + CLI, not an OpenTelemetry Collector processor

Status: accepted · Date: 2026-07-02

## Context

The suite needs to receive OTLP traffic and validate it against the GenAI
semantic conventions. Three packaging options were considered:

1. An OpenTelemetry Collector processor, built and distributed via the
   OpenTelemetry Collector Builder (OCB).
2. A standalone Go service that speaks OTLP natively, plus an offline CLI.
3. An offline CLI only (no live ingest path).

## Decision

Build **one standalone binary** (`genai-conformance`) with `serve`,
`validate`, and `rules` subcommands, all sharing a single conformance
engine library. Use `go.opentelemetry.io/collector/pdata` so the wire
types are byte-for-byte the ones the collector itself uses.

Reasons:

- The offline `validate` mode is a first-class CI gate (exit codes, JUnit
  output). A collector processor has no offline story at all.
- A single static binary with an embedded report UI is trivially runnable
  by anyone (`go run`, one docker image); an OCB pipeline couples the
  build and release cadence to collector releases.
- Trace-topology rules need cross-batch trace assembly with explicit
  memory bounds; owning the service boundary keeps that logic and its
  self-metrics in one place instead of fighting processor lifecycle
  semantics.

## Consequences

- Deployments that already run a collector add one extra OTLP hop (the
  gateway can optionally forward traffic onward, so it can sit in-line).
- Because the engine is a plain library (`internal/engine` + `internal/rules`),
  wrapping it as a collector processor later is a packaging exercise, not a
  rewrite. This is the top roadmap item, deliberately not built now.

## Related work (and why this exists anyway)

- **`weaver registry live-check`** validates live telemetry attributes
  against a semconv registry. It is registry-driven and generic; it does not
  do trace-topology checks (agent → tool → LLM ancestry, orphans, timing),
  cross-signal correlation (evaluation events joined to the traces they
  score), severity-mapped conformance scoring, or GenAI-specific migration
  hints. This suite is opinionated about *GenAI application telemetry* as a
  whole, not individual attributes.
- **`semantic-conventions-genai/reference/`** compliance matrices validate
  *instrumentation libraries* against mock provider endpoints. This suite
  validates *your application's* emitted telemetry, whatever SDK or
  framework produced it.
- **openllmetry / openinference** are instrumentation ecosystems — they
  *emit* GenAI telemetry. This project sits on the other side of the wire
  and *judges* what was emitted.
