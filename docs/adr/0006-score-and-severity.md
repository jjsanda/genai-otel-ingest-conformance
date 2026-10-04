# ADR-0006: Score definition and severity mapping

Status: accepted · Date: 2026-07-03

## Context

"Conformance score" is only useful if it is precise enough to assert in
CI and stable enough to alert on. A vague "percentage of good telemetry"
invites gaming and flapping.

## Decision

**One check = one scoreable rule applied to one applicable entity**
(span, resource, metric, log record, or assembled trace). Per service:

    score = passed checks / total checks

Severity maps from the conventions' requirement language:

| Severity | Meaning | In score? |
|---|---|---|
| ERROR | violates Required / decidable Conditionally Required — broken for standards-based consumers | yes |
| WARNING | violates Recommended (SHOULD) | yes |
| INFO | legal but noteworthy (custom provider names, content capture present, bucket advice) | **no** |

INFO is excluded so hints never punish a score: the mock-provider demo
scores a perfect 1.0 while still surfacing "custom provider name" hints.
`validate` exits 1 on ERROR findings (or any finding with `--strict`),
0 otherwise, 2 on usage errors — the CI contract.

Conditionally-required attributes are enforced only when the condition
is decidable from telemetry alone (`error.type` when status is Error,
`server.port` when `server.address` is set); conditions like "if
available" are unknowable from the wire and are documentation, not
findings — a deliberate false-positive-avoidance stance.

Reports deduplicate: the live gateway keeps per-rule counts plus at most
five exemplar findings, so a firehose of identical violations stays
readable.

## Consequences

- Scores are deterministic for deterministic input — golden tests assert
  exact values (the demo's offline run is exactly 717/717).
- A service emitting very little telemetry can swing hard on one bad
  span; that is honest (small sample, small confidence) and visible via
  the check count next to every score.
- Weighting ERROR above WARNING inside one number was rejected: two
  numbers people understand (score + error count) beat one number
  nobody can explain.
