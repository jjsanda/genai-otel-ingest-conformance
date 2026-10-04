# Conformance rule catalog

<!-- GENERATED FILE — do not edit. Regenerate with `make docs`
     (runs `genai-conformance rules list --format markdown`). -->

32 rules, pinned to [open-telemetry/semantic-conventions-genai@`b028dceecdad`](https://github.com/open-telemetry/semantic-conventions-genai/tree/b028dceecdad117461a785c3af35315e7184e813) (2026-06-28).

Severity mapping: **ERROR** violates a Required (or decidable Conditionally
Required) convention — the telemetry is broken for standards-based consumers;
**WARNING** violates a Recommended (SHOULD) convention; **INFO** is a hint about
legal-but-noteworthy telemetry. The conformance score counts ERROR- and
WARNING-level checks; INFO never affects it.

## Span rules

Checked streaming, per span, as telemetry arrives.

| ID | Severity | Checks for |
|---|---|---|
| [GENAI-SPAN-001](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | GenAI spans must carry gen_ai.operation.name and the operation's required attributes (span-kind aware). |
| [GENAI-SPAN-002](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | gen_ai.operation.name should be one of the predefined operation values. |
| [GENAI-SPAN-003](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Span names should follow the operation's format (e.g. "chat {gen_ai.request.model}"). |
| [GENAI-SPAN-004](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Span kind should match the operation's convention (CLIENT for inference, INTERNAL for tool execution, ...). |
| [GENAI-SPAN-005](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | Attribute values must use the wire type the conventions define. |
| [GENAI-SPAN-006](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | Numeric attribute values must be within legal bounds (token counts are never negative). |
| [GENAI-SPAN-007](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | error.type is required when the span status is Error. |
| [GENAI-SPAN-008](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | server.port is required when server.address is set. |
| [GENAI-SPAN-009](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | Deprecated attributes must be migrated to their replacements (gen_ai.system → gen_ai.provider.name, ...). |
| [GENAI-SPAN-010](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Key recommended attributes should be present: token usage and response metadata drive cost and drift dashboards. |
| [GENAI-SPAN-011](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | INFO | Content-capture attributes detected; they are opt-in and may carry prompts/PII — confirm the opt-in and redaction policy. |
| [GENAI-SPAN-012](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | Legacy content span events (gen_ai.content.prompt, gen_ai.choice, ...) must be migrated to content attributes. |
| [GENAI-SPAN-013](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | ERROR | Closed-enum attributes (gen_ai.output.type, gen_ai.token.type) must use a defined value. |
| [GENAI-SPAN-014](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | INFO | Open-enum attribute uses a custom value (permitted; well-known values integrate better). |
| [GENAI-SPAN-015](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Unknown gen_ai.* attribute: likely a typo, or telemetry from a newer conventions snapshot than the pin. |

## Trace-topology rules

Checked per assembled trace (the gateway's closed windows, or the full input offline): request → agent → tool → retrieval → LLM continuity.

| ID | Severity | Checks for |
|---|---|---|
| [GENAI-TRACE-001](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-agent-spans.md) | WARNING | execute_tool spans should descend from an invoke_agent/invoke_workflow/plan span when the trace has one. |
| [GENAI-TRACE-002](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Spans referencing a parent that was never received (broken export pipeline, or the parent's service exports elsewhere). |
| [GENAI-TRACE-003](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Traces should include a root span (the entry point that triggered the GenAI work). |
| [GENAI-TRACE-004](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Child spans should lie within their parent's time bounds (5ms skew tolerance). |
| [GENAI-TRACE-005](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md) | WARNING | Spans arrived after their trace window closed (upstream batching/export likely mis-tuned). |

## Metric rules

Checked per metric: instrument shapes, units, required attributes, bucket advice.

| ID | Severity | Checks for |
|---|---|---|
| [GENAI-METRIC-001](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-metrics.md) | ERROR | GenAI metrics must use the defined instrument type and unit (e.g. histogram in "s"). |
| [GENAI-METRIC-002](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-metrics.md) | ERROR | Required metric attributes (e.g. gen_ai.token.type on token usage) must be present on every data point, with legal values. |
| [GENAI-METRIC-003](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-metrics.md) | WARNING | Unknown gen_ai.* metric name: likely a typo, or a newer conventions snapshot than the pin. |
| [GENAI-METRIC-004](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-metrics.md) | INFO | Histogram bucket boundaries differ from the conventions' advice (legal, but hurts cross-service comparability). |
| [GENAI-METRIC-005](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-metrics.md) | WARNING | Seconds-unit duration values exceed one hour — milliseconds recorded as seconds? |

## Event rules

Checked per GenAI log record, including evaluation results.

| ID | Severity | Checks for |
|---|---|---|
| [GENAI-EVENT-001](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-events.md) | ERROR | GenAI events must carry their required attributes (e.g. gen_ai.evaluation.name on evaluation results). |
| [GENAI-EVENT-002](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-events.md) | WARNING | Evaluation events should be parented to the evaluated span (trace/span id) or carry gen_ai.response.id. |
| [GENAI-EVENT-003](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-events.md) | WARNING | Unknown gen_ai.* event name: a typo, a legacy shape, or a newer conventions snapshot than the pin. |
| [GENAI-EVENT-004](https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-exceptions.md) | WARNING | gen_ai.client.operation.exception events should be recorded at severity WARN (13). |

## Resource rules

Checked once per resource that emits GenAI telemetry.

| ID | Severity | Checks for |
|---|---|---|
| [GENAI-RES-001](https://opentelemetry.io/docs/specs/semconv/resource/#service) | ERROR | service.name must be set to a real value (not unknown_service). |
| [GENAI-RES-002](https://opentelemetry.io/docs/specs/semconv/resource/#service) | WARNING | service.version should be set; without it regressions cannot be tied to deployments. |
| [GENAI-RES-003](https://opentelemetry.io/docs/specs/semconv/resource/#telemetry-sdk) | INFO | telemetry.sdk.* attributes are absent; official SDKs set them automatically. |
