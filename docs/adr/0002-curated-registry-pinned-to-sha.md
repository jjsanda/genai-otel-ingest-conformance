# ADR-0002: Hand-curated conformance registry pinned to an upstream commit SHA

Status: accepted · Date: 2026-07-02

## Context

The GenAI semantic conventions live in a dedicated repository
(`open-telemetry/semantic-conventions-genai`) that is **Development
status with no tagged releases**: attribute names, requirement levels,
and event shapes still change. Recent history includes renames such as
`gen_ai.system` → `gen_ai.provider.name` and `gen_ai.usage.prompt_tokens`
→ `gen_ai.usage.input_tokens`.

Conformance rules need more than the upstream Weaver YAML can express:
per-operation requirement matrices (e.g. `gen_ai.provider.name` is
required on CLIENT inference spans but not on INTERNAL `execute_tool`
spans), severity mapping (MUST/SHOULD/opt-in → ERROR/WARNING/INFO),
remediation hints, and stable documentation links.

Two approaches were considered:

1. Generate the registry from upstream `model/*.yaml` with Weaver at build
   time.
2. Hand-curate a registry, pinned to a specific upstream commit, with the
   upstream YAML vendored alongside for drift detection.

## Decision

Hand-curate the registry (`internal/registry/curated/*.yaml`, embedded via
`go:embed`), **pinned to upstream commit `b028dce` (2026-06-28)**. Every
entry carries a documentation link anchored to that SHA so links never rot.

Vendor the upstream `model/gen-ai/*.yaml` (plus its Apache-2.0 license) at
the same SHA under `third_party/semconv-genai/` via `hack/sync-semconv.sh`.
A weekly, non-blocking `semconv-drift` workflow re-fetches upstream `main`
and reports a human-readable delta against the pin.

Weaver generation was rejected because: upstream YAML is not self-resolving
(its manifest pulls the core semconv registry over the network at resolve
time), generated output cannot carry severity/remediation/applicability
metadata, and a Weaver build dependency buys little for a registry this
size.

## Consequences

- Manual transcription is a risk; it is mitigated by registry unit tests
  that cross-check curated entries against the vendored upstream YAML, and
  by the drift workflow making upstream movement visible instead of silent.
- Upgrading the pin is an explicit, reviewable commit: rerun
  `hack/sync-semconv.sh <new-sha>`, update curated entries, update goldens.
- The registry format carries its own version so multiple semconv snapshots
  can coexist later (roadmap; out of scope for v0.1).
