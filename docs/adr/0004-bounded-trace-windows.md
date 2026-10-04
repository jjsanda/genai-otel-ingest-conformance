# ADR-0004: Bounded trace windows with explicit late-arrival semantics

Status: accepted · Date: 2026-07-03

## Context

Trace-topology rules (agent → tool ancestry, orphaned parents, missing
roots, child timing) need whole traces, but OTLP delivers spans in
arbitrary batches: a trace's spans arrive interleaved across requests,
services, and exporter flush cycles. A gateway that buffers "until the
trace is complete" without bounds is a memory leak with extra steps —
there is no completeness signal in OTLP.

## Decision

Assemble spans into per-trace windows with four explicit bounds, all
flags:

| Bound | Default | On breach |
|---|---|---|
| Idle timeout | 10s without new spans | close and judge |
| Max age | 60s after first span | close and judge |
| Open windows | 10,000 (LRU) | evict oldest, mark `Incomplete` |
| Spans per trace | 2,000 | drop excess, mark `Incomplete` |

`Incomplete` traces skip topology rules — a partial trace would produce
false orphan/root findings, and a conformance tool that cries wolf gets
turned off.

**Late spans never reopen a closed window.** They start a fresh window
marked `LateArrival`, which fires GENAI-TRACE-005: telemetry arriving
after its trace was judged means upstream batching or export timeouts
are mis-tuned relative to trace duration — a real operational signal,
observed immediately when the compose demo's traffic generator flushed
on the same 5s cadence as an experimental 5s idle window.

Concurrency is one mutex around a map plus an LRU list, swept by a 1s
ticker. At gateway scale (thousands of open traces) the critical
sections are microseconds; sharding or an actor model would be
speculative complexity with nothing measured to justify it.

## Consequences

- Report latency is bounded by idle timeout (and hard-capped by max
  age); memory is bounded by the two caps. Both are observable:
  `genai_conformance_traces_open`, `_spans_buffered`,
  `_windows_closed_total{reason}`, `_late_spans_total`.
- A trace slower than the max age is judged in pieces (first window
  complete, remainder LateArrival). The defaults leave 6× headroom over
  the demo's longest traces; real deployments tune the flags.
- Offline `validate` needs none of this: its input files are the
  complete universe of spans, so traces assemble exactly.
