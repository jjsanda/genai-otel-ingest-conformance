# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[SemVer](https://semver.org/).

## [0.1.0] — 2026-07-03

First complete vertical slice.

### Added

- **Conformance engine** with a 32-rule catalog pinned to
  [`open-telemetry/semantic-conventions-genai@b028dce`](https://github.com/open-telemetry/semantic-conventions-genai/tree/b028dceecdad117461a785c3af35315e7184e813):
  span rules (per-operation, span-kind-aware requirement matrices, naming,
  types, deprecations, content-capture hints), trace-topology rules
  (agent→tool ancestry, orphans, roots, timing, late arrivals), metric rules
  (shapes, units, required attributes, bucket advice, a seconds-vs-
  milliseconds heuristic), event rules (evaluation results, exceptions), and
  resource identity rules.
- **`validate`** — offline CI gate reading OTLP protobuf/JSON/JSONL with
  markdown, JSON, and JUnit reports and a 0/1/2 exit contract; ships as a
  reusable composite GitHub Action (`action.yml`).
- **`serve`** — live OTLP gateway (gRPC + HTTP) with bounded trace-window
  assembly, per-service scores, an embedded report UI, `/api/report`,
  `/api/badge.svg`, and Prometheus self-metrics.
- **Curated semconv registry** hand-transcribed from the pinned upstream
  model, cross-checked by tests against the vendored YAML, with a weekly
  drift-watch workflow.
- **Demo apps**: a hand-instrumented FastAPI agent (mock-first LLMs,
  retrieval, evaluation events, deliberate `--noncompliant` mode) and a real
  LangGraph app bridged via a custom LangChain callback handler. Both score
  1.0; the broken mode is caught with a golden finding set enforced in CI.
- **Deployments**: one-command docker-compose stack (collector remediation
  path vs direct path, Prometheus, provisioned Grafana dashboard) and
  kustomize manifests with a kind e2e suite asserting live scores.
