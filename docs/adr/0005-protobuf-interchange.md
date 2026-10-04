# ADR-0005: Protobuf as the cross-language interchange format

Status: accepted · Date: 2026-07-03

## Context

The CI gate's core move is cross-language: Python demos export recorded
telemetry to files, and the Go engine validates them. The obvious
"just use JSON" has a trap: **the OTLP/JSON spec requires trace and span
IDs to be hex-encoded, but protobuf's canonical JSON mapping (what
`MessageToJson` and most language protobuf runtimes emit) encodes
`bytes` fields as base64.** Telemetry serialized with a stock protobuf
JSON printer is not OTLP/JSON, and consumers that follow the spec
(including pdata's unmarshaler) reject or mangle the IDs.

## Decision

**OTLP protobuf bytes (`.binpb`) are the primary interchange format.**
Binary protobuf has exactly one encoding; there is nothing to get wrong.
Python writes `ExportRequest.SerializeToString()` via the
`opentelemetry-exporter-otlp-proto-common` encoders; Go reads with
`ptraceotlp.ExportRequest.UnmarshalProto` (and the metric/log
equivalents). File names carry the signal (`*.traces.binpb`,
`*.metrics.binpb`, `*.logs.binpb`) because the three request messages
are not distinguishable by content.

`validate` additionally accepts spec-correct OTLP/JSON (`.json`) and
collector file-exporter lines (`.jsonl`) — hand-written JSON fixtures
double as spec-conformance tests for the decoder — but generated
interchange always uses protobuf.

## Consequences

- The cross-language gate is byte-exact and immune to JSON dialect
  drift; the hex-vs-base64 divergence is documented here instead of
  discovered at 2am.
- Binary fixtures are not human-readable; the JSON fixture corpus under
  `testdata/` covers the read-it-in-review need.
