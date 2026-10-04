package rules

import (
	"fmt"
	"time"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// Trace-topology rules run over assembled traces (the gateway's closed
// windows, or the full input set in offline validate). They are the reason
// this suite exists beyond per-span attribute checking: "request → agent →
// tool → retrieval → LLM" continuity is only checkable across spans.
//
// Rules 001–004 skip traces marked Incomplete (evicted or truncated) — a
// partial trace would produce false topology findings. Rule 005 exists
// precisely for the late-arrival case.

// agenticOps are operations that own child GenAI operations.
var agenticOps = map[string]bool{"invoke_agent": true, "invoke_workflow": true, "plan": true}

// completeOnly is embedded by the topology rules that can only judge a fully
// assembled trace. Incomplete (evicted/truncated) or late-arriving traces
// are not-applicable to them — the engine skips such (rule, trace) pairs so
// a skip is never miscounted as a passed check.
type completeOnly struct{}

func (completeOnly) AppliesToTrace(tc *engine.TraceContext) bool {
	return !tc.Incomplete && !tc.LateArrival
}

// traceToolAncestry implements GENAI-TRACE-001: when a trace contains agent
// or workflow spans, tool executions should be descendants of one — a tool
// span floating outside the agent breaks the "request → agent → tool" story
// consumers reconstruct.
type traceToolAncestry struct {
	engine.Base
	completeOnly
	reg *registry.Registry
}

func newTraceToolAncestry(reg *registry.Registry) engine.Rule {
	return &traceToolAncestry{
		Base: engine.Base{
			RuleID:   "GENAI-TRACE-001",
			Sev:      engine.SeverityWarning,
			RuleNote: "execute_tool spans should descend from an invoke_agent/invoke_workflow/plan span when the trace has one.",
			RuleDoc:  reg.DocURL("gen-ai-agent-spans.md"),
		},
		reg: reg,
	}
}

func (r *traceToolAncestry) CheckTrace(tc *engine.TraceContext) []engine.Finding {
	hasAgent := false
	for _, s := range tc.Spans {
		if agenticOps[s.OperationName()] {
			hasAgent = true
			break
		}
	}
	if !hasAgent {
		return nil // a bare LLM-call app without agents is perfectly legal
	}
	var out []engine.Finding
	for _, s := range tc.Spans {
		if s.OperationName() != "execute_tool" {
			continue
		}
		if !hasAncestorOp(tc, s, agenticOps) {
			out = append(out, tc.NewFinding(r, s,
				"execute_tool span is not a descendant of any invoke_agent/invoke_workflow/plan span in this trace",
				"start tool spans inside the agent invocation's context so the agent → tool relationship survives"))
		}
	}
	return out
}

// traceOrphanParents implements GENAI-TRACE-002: a span whose parent never
// arrived means a broken exporter, a dropped span, or context propagated
// into telemetry that was never emitted.
type traceOrphanParents struct {
	engine.Base
	completeOnly
	reg *registry.Registry
}

func newTraceOrphanParents(reg *registry.Registry) engine.Rule {
	return &traceOrphanParents{
		Base: engine.Base{
			RuleID:   "GENAI-TRACE-002",
			Sev:      engine.SeverityWarning,
			RuleNote: "Spans referencing a parent that was never received (broken export pipeline, or the parent's service exports elsewhere).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *traceOrphanParents) CheckTrace(tc *engine.TraceContext) []engine.Finding {
	var out []engine.Finding
	for _, s := range tc.Spans {
		parent := s.Span.ParentSpanID()
		if parent.IsEmpty() {
			continue
		}
		if _, ok := tc.ByID[parent.String()]; !ok {
			out = append(out, tc.NewFinding(r, s,
				fmt.Sprintf("parent span %s was never received for this trace", parent),
				"check that every service in the request path exports to the same destination and that no sampler drops parents of GenAI spans"))
		}
	}
	return out
}

// traceHasRoot implements GENAI-TRACE-003: an observed trace with no root
// span has no entry point — the request that triggered the agent was not
// instrumented or not exported here.
type traceHasRoot struct {
	engine.Base
	completeOnly
	reg *registry.Registry
}

func newTraceHasRoot(reg *registry.Registry) engine.Rule {
	return &traceHasRoot{
		Base: engine.Base{
			RuleID:   "GENAI-TRACE-003",
			Sev:      engine.SeverityWarning,
			RuleNote: "Traces should include a root span (the entry point that triggered the GenAI work).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *traceHasRoot) CheckTrace(tc *engine.TraceContext) []engine.Finding {
	for _, s := range tc.Spans {
		if s.Span.ParentSpanID().IsEmpty() {
			return nil
		}
	}
	return []engine.Finding{tc.NewFinding(r, nil,
		"no root span observed for this trace; the entry point (HTTP request, queue consumer, job) is missing",
		"instrument the request entry point and export it to the same destination — GENAI-TRACE-002 findings show which parents are missing")}
}

// clockSkewTolerance absorbs cross-host clock drift before timing findings
// fire; anything beyond this is a real instrumentation bug, not skew.
const clockSkewTolerance = 5 * time.Millisecond

// traceChildTiming implements GENAI-TRACE-004: children outside their
// parent's time bounds (beyond skew tolerance) usually mean timestamps taken
// from the wrong clock or spans ended after their parent was closed.
type traceChildTiming struct {
	engine.Base
	completeOnly
	reg *registry.Registry
}

func newTraceChildTiming(reg *registry.Registry) engine.Rule {
	return &traceChildTiming{
		Base: engine.Base{
			RuleID:   "GENAI-TRACE-004",
			Sev:      engine.SeverityWarning,
			RuleNote: "Child spans should lie within their parent's time bounds (5ms skew tolerance).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *traceChildTiming) CheckTrace(tc *engine.TraceContext) []engine.Finding {
	var out []engine.Finding
	for _, s := range tc.Spans {
		parentID := s.Span.ParentSpanID()
		if parentID.IsEmpty() {
			continue
		}
		parent, ok := tc.ByID[parentID.String()]
		if !ok {
			continue // GENAI-TRACE-002's finding
		}
		if !s.IsGenAI() && !parent.IsGenAI() {
			continue
		}
		childStart := s.Span.StartTimestamp().AsTime()
		childEnd := s.Span.EndTimestamp().AsTime()
		parentStart := parent.Span.StartTimestamp().AsTime()
		parentEnd := parent.Span.EndTimestamp().AsTime()
		if childStart.Before(parentStart.Add(-clockSkewTolerance)) || childEnd.After(parentEnd.Add(clockSkewTolerance)) {
			out = append(out, tc.NewFinding(r, s,
				fmt.Sprintf("span runs %s → %s, outside its parent %q (%s → %s)",
					childStart.Format(time.RFC3339Nano), childEnd.Format(time.RFC3339Nano),
					parent.Span.Name(), parentStart.Format(time.RFC3339Nano), parentEnd.Format(time.RFC3339Nano)),
				"take start/end timestamps from the span's own lifecycle and end child spans before their parent"))
		}
	}
	return out
}

// traceLateArrival implements GENAI-TRACE-005: spans arriving after their
// trace's window closed are themselves a conformance signal — upstream
// batching or export timeouts are mis-tuned relative to trace duration.
// This rule only ever fires in the live gateway; offline validation sees
// the complete input and never marks windows late.
type traceLateArrival struct {
	engine.Base
	reg *registry.Registry
}

func newTraceLateArrival(reg *registry.Registry) engine.Rule {
	return &traceLateArrival{
		Base: engine.Base{
			RuleID:   "GENAI-TRACE-005",
			Sev:      engine.SeverityWarning,
			RuleNote: "Spans arrived after their trace window closed (upstream batching/export likely mis-tuned).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

// AppliesToTrace makes GENAI-TRACE-005 the mirror image of the topology
// rules: it judges only late-arrival windows, so on-time traces record no
// check for it (and never a spurious pass or fail).
func (r *traceLateArrival) AppliesToTrace(tc *engine.TraceContext) bool {
	return tc.LateArrival
}

func (r *traceLateArrival) CheckTrace(tc *engine.TraceContext) []engine.Finding {
	return []engine.Finding{tc.NewFinding(r, nil,
		fmt.Sprintf("%d span(s) for this trace arrived after its assembly window had closed and were judged separately", len(tc.Spans)),
		"lower exporter batch delays or raise the gateway's --window-idle so complete traces are judged together")}
}

// hasAncestorOp walks the parent chain looking for an operation in ops.
func hasAncestorOp(tc *engine.TraceContext, s *engine.SpanContext, ops map[string]bool) bool {
	seen := map[string]bool{}
	current := s
	for {
		parentID := current.Span.ParentSpanID()
		if parentID.IsEmpty() {
			return false
		}
		id := parentID.String()
		if seen[id] { // defensive: a parent cycle must not hang the gateway
			return false
		}
		seen[id] = true
		parent, ok := tc.ByID[id]
		if !ok {
			return false
		}
		if ops[parent.OperationName()] {
			return true
		}
		current = parent
	}
}
