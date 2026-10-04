package engine

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

type fakeSpanRule struct {
	Base
	fail bool
}

func (r *fakeSpanRule) CheckSpan(c *SpanContext) []Finding {
	if r.fail {
		return []Finding{c.NewFinding(r, "x", "boom", "fix it")}
	}
	return nil
}

func newFake(id string, sev Severity, fail bool) *fakeSpanRule {
	return &fakeSpanRule{Base: Base{RuleID: id, Sev: sev, RuleNote: "fake", RuleDoc: "http://x"}, fail: fail}
}

func testTraces(service string, genAI bool) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	if service != "" {
		rs.Resource().Attributes().PutStr("service.name", service)
	}
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("chat gpt-4")
	span.SetKind(ptrace.SpanKindClient)
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	if genAI {
		span.Attributes().PutStr("gen_ai.operation.name", "chat")
	}
	return td
}

func TestTallyCountsAndInfoExclusion(t *testing.T) {
	reg := registry.Default()
	eng := New(reg,
		newFake("T-001", SeverityError, true),
		newFake("T-002", SeverityWarning, false),
		newFake("T-003", SeverityInfo, true),
	)
	res := eng.EvaluateTraces(testTraces("svc-a", true))

	tally, ok := res.Tallies["svc-a"]
	if !ok {
		t.Fatal("no tally for svc-a")
	}
	// ERROR and WARNING rules are scoreable; the INFO rule is not.
	if tally.Checks != 2 || tally.Passed != 1 {
		t.Errorf("tally = %d/%d, want passed 1 of checks 2", tally.Passed, tally.Checks)
	}
	// Findings include the INFO one.
	if len(res.Findings) != 2 {
		t.Errorf("findings = %d, want 2 (one ERROR, one INFO)", len(res.Findings))
	}
	if got := tally.Score(); got != 0.5 {
		t.Errorf("score = %v, want 0.5", got)
	}
}

func TestNonGenAISpansAreSkipped(t *testing.T) {
	reg := registry.Default()
	eng := New(reg, newFake("T-001", SeverityError, true))
	res := eng.EvaluateTraces(testTraces("svc-a", false))
	if len(res.Findings) != 0 || len(res.Tallies) != 0 {
		t.Errorf("non-GenAI span must not be evaluated: %+v", res)
	}
}

func TestServiceFallsBackToUnknown(t *testing.T) {
	reg := registry.Default()
	eng := New(reg, newFake("T-001", SeverityError, true))
	res := eng.EvaluateTraces(testTraces("", true))
	if _, ok := res.Tallies[unknownService]; !ok {
		t.Errorf("expected tally under %q, got %v", unknownService, res.Tallies)
	}
}

func TestMerge(t *testing.T) {
	a := NewResult()
	a.record("svc", SeverityError, nil)
	b := NewResult()
	b.record("svc", SeverityError, []Finding{{RuleID: "X"}})
	a.Merge(b)
	if a.Tallies["svc"].Checks != 2 || a.Tallies["svc"].Passed != 1 {
		t.Errorf("merged tally = %+v", a.Tallies["svc"])
	}
	if len(a.Findings) != 1 {
		t.Errorf("merged findings = %d, want 1", len(a.Findings))
	}
}

func TestAssembleTracesGroupsAcrossBatches(t *testing.T) {
	reg := registry.Default()
	eng := New(reg, newFake("T-001", SeverityError, false))

	a := testTraces("svc-a", true)
	b := testTraces("svc-b", true)
	// Same trace ID in both batches (testTraces uses a fixed ID); distinct span IDs.
	b.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).SetSpanID(pcommon.SpanID([8]byte{9, 9, 9, 9, 9, 9, 9, 9}))

	groups := eng.AssembleTraces([]ptrace.Traces{a, b})
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if len(groups[0].Spans) != 2 {
		t.Errorf("spans in trace = %d, want 2", len(groups[0].Spans))
	}
	if groups[0].ByID["0102030405060708"] == nil || groups[0].ByID["0909090909090909"] == nil {
		t.Errorf("ByID index incomplete: %v", groups[0].ByID)
	}
}

func TestEmptyTallyScoresPerfect(t *testing.T) {
	if got := (Tally{}).Score(); got != 1 {
		t.Errorf("empty tally score = %v, want 1", got)
	}
}

type fakeTraceRule struct {
	Base
	applies bool
	fail    bool
}

func (r *fakeTraceRule) CheckTrace(tc *TraceContext) []Finding {
	if r.fail {
		return []Finding{tc.NewFinding(r, nil, "boom", "fix")}
	}
	return nil
}
func (r *fakeTraceRule) AppliesToTrace(*TraceContext) bool { return r.applies }

func TestNotApplicableTraceRuleRecordsNoCheck(t *testing.T) {
	reg := registry.Default()
	applicablePass := &fakeTraceRule{Base: Base{RuleID: "TR-1", Sev: SeverityWarning}, applies: true, fail: false}
	skipped := &fakeTraceRule{Base: Base{RuleID: "TR-2", Sev: SeverityWarning}, applies: false, fail: false}
	eng := New(reg, applicablePass, skipped)

	tc := eng.AssembleTraces([]ptrace.Traces{testTraces("svc", true)})[0]
	res := eng.EvaluateTrace(tc)

	// Only the applicable rule contributes a check; the skipped rule must
	// not be counted as a passed check.
	if got := res.Tallies["svc"].Checks; got != 1 {
		t.Errorf("checks = %d, want 1 (skipped rule must not count)", got)
	}
}
