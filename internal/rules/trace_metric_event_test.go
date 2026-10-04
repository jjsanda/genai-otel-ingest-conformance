package rules

import (
	"testing"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanSpec is a compact multi-span trace builder for topology tests.
type spanSpec struct {
	id, parent byte // span IDs as single distinguishing bytes; 0 parent = root
	op         string
	name       string
	start, end int64 // unix nanos
}

func buildTrace(specs []spanSpec) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "topo-test")
	ss := rs.ScopeSpans().AppendEmpty()
	for _, sp := range specs {
		span := ss.Spans().AppendEmpty()
		span.SetTraceID(pcommon.TraceID([16]byte{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}))
		span.SetSpanID(pcommon.SpanID([8]byte{sp.id, 0, 0, 0, 0, 0, 0, 1}))
		if sp.parent != 0 {
			span.SetParentSpanID(pcommon.SpanID([8]byte{sp.parent, 0, 0, 0, 0, 0, 0, 1}))
		}
		span.SetName(sp.name)
		span.SetKind(ptrace.SpanKindInternal)
		span.Attributes().PutStr("gen_ai.operation.name", sp.op)
		if sp.op == "execute_tool" {
			span.Attributes().PutStr("gen_ai.tool.name", "t")
		}
		span.SetStartTimestamp(pcommon.Timestamp(sp.start))
		span.SetEndTimestamp(pcommon.Timestamp(sp.end))
	}
	return td
}

func evalTraceRule(t *testing.T, factory func(*registry.Registry) engine.Rule, specs []spanSpec, mutate func(tc *engine.TraceContext)) []engine.Finding {
	t.Helper()
	reg := registry.Default()
	eng := engine.New(reg, factory(reg))
	groups := eng.AssembleTraces([]ptrace.Traces{buildTrace(specs)})
	if len(groups) != 1 {
		t.Fatalf("expected 1 assembled trace, got %d", len(groups))
	}
	if mutate != nil {
		mutate(groups[0])
	}
	return eng.EvaluateTrace(groups[0]).Findings
}

func TestToolAncestryWithinAgent(t *testing.T) {
	base := int64(1_000_000_000)
	nested := evalTraceRule(t, newTraceToolAncestry, []spanSpec{
		{id: 1, op: "invoke_agent", name: "invoke_agent a", start: base, end: base + 100},
		{id: 2, parent: 1, op: "execute_tool", name: "execute_tool t", start: base + 10, end: base + 20},
	}, nil)
	if len(nested) != 0 {
		t.Errorf("tool under agent must pass, got %v", nested)
	}

	floating := evalTraceRule(t, newTraceToolAncestry, []spanSpec{
		{id: 1, op: "invoke_agent", name: "invoke_agent a", start: base, end: base + 100},
		{id: 2, op: "execute_tool", name: "execute_tool t", start: base + 10, end: base + 20}, // root-level tool
	}, nil)
	if len(floating) != 1 {
		t.Errorf("tool outside agent (agent present) must be flagged, got %v", floating)
	}

	noAgent := evalTraceRule(t, newTraceToolAncestry, []spanSpec{
		{id: 2, op: "execute_tool", name: "execute_tool t", start: base, end: base + 20},
	}, nil)
	if len(noAgent) != 0 {
		t.Errorf("trace without any agent span must not be flagged, got %v", noAgent)
	}
}

func TestOrphanAndRootRules(t *testing.T) {
	base := int64(1_000_000_000)
	orphaned := []spanSpec{
		{id: 2, parent: 9, op: "chat", name: "chat", start: base, end: base + 10},
	}
	if got := evalTraceRule(t, newTraceOrphanParents, orphaned, nil); len(got) != 1 {
		t.Errorf("missing parent must be flagged, got %v", got)
	}
	if got := evalTraceRule(t, newTraceHasRoot, orphaned, nil); len(got) != 1 {
		t.Errorf("trace without a root must be flagged, got %v", got)
	}

	rooted := []spanSpec{
		{id: 1, op: "invoke_agent", name: "invoke_agent a", start: base, end: base + 100},
		{id: 2, parent: 1, op: "chat", name: "chat", start: base + 1, end: base + 50},
	}
	if got := evalTraceRule(t, newTraceOrphanParents, rooted, nil); len(got) != 0 {
		t.Errorf("intact parentage must pass, got %v", got)
	}
	if got := evalTraceRule(t, newTraceHasRoot, rooted, nil); len(got) != 0 {
		t.Errorf("rooted trace must pass, got %v", got)
	}
}

func TestChildTimingBounds(t *testing.T) {
	base := int64(1_000_000_000)
	escaping := []spanSpec{
		{id: 1, op: "invoke_agent", name: "invoke_agent a", start: base, end: base + 100_000_000},
		{id: 2, parent: 1, op: "chat", name: "chat", start: base + 1, end: base + 900_000_000}, // ends after parent
	}
	if got := evalTraceRule(t, newTraceChildTiming, escaping, nil); len(got) != 1 {
		t.Errorf("child escaping parent bounds must be flagged, got %v", got)
	}

	skew := []spanSpec{
		{id: 1, op: "invoke_agent", name: "invoke_agent a", start: base, end: base + 100_000_000},
		{id: 2, parent: 1, op: "chat", name: "chat", start: base - 2_000_000, end: base + 50_000_000}, // 2ms early: within tolerance
	}
	if got := evalTraceRule(t, newTraceChildTiming, skew, nil); len(got) != 0 {
		t.Errorf("clock skew within tolerance must pass, got %v", got)
	}
}

func TestIncompleteAndLateTraceHandling(t *testing.T) {
	base := int64(1_000_000_000)
	orphaned := []spanSpec{{id: 2, parent: 9, op: "chat", name: "chat", start: base, end: base + 10}}

	// Incomplete traces must not produce topology findings.
	got := evalTraceRule(t, newTraceOrphanParents, orphaned, func(tc *engine.TraceContext) { tc.Incomplete = true })
	if len(got) != 0 {
		t.Errorf("incomplete trace must skip topology checks, got %v", got)
	}

	// Late windows fire exactly the late-arrival rule.
	late := evalTraceRule(t, newTraceLateArrival, orphaned, func(tc *engine.TraceContext) { tc.LateArrival = true })
	if len(late) != 1 {
		t.Errorf("late window must be flagged, got %v", late)
	}
	if got := evalTraceRule(t, newTraceLateArrival, orphaned, nil); len(got) != 0 {
		t.Errorf("on-time trace must not trigger the late rule, got %v", got)
	}
}

func evalMetricRule(t *testing.T, factory func(*registry.Registry) engine.Rule, build func(m pmetric.Metric)) []engine.Finding {
	t.Helper()
	reg := registry.Default()
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "metric-test")
	m := rm.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	build(m)
	eng := engine.New(reg, factory(reg))
	return eng.EvaluateMetrics(md).Findings
}

func tokenUsageHistogram(m pmetric.Metric, unit string, withTokenType bool) {
	m.SetName("gen_ai.client.token.usage")
	m.SetUnit(unit)
	dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
	dp.Attributes().PutStr("gen_ai.operation.name", "chat")
	dp.Attributes().PutStr("gen_ai.provider.name", "openai")
	if withTokenType {
		dp.Attributes().PutStr("gen_ai.token.type", "input")
	}
}

func TestMetricShapeAndRequiredAttrs(t *testing.T) {
	wrongUnit := evalMetricRule(t, newMetricShape, func(m pmetric.Metric) { tokenUsageHistogram(m, "1", true) })
	if len(wrongUnit) != 1 {
		t.Errorf("wrong unit must be flagged, got %v", wrongUnit)
	}

	wrongType := evalMetricRule(t, newMetricShape, func(m pmetric.Metric) {
		m.SetName("gen_ai.client.token.usage")
		m.SetUnit("{token}")
		m.SetEmptySum().DataPoints().AppendEmpty()
	})
	if len(wrongType) != 1 {
		t.Errorf("sum instead of histogram must be flagged, got %v", wrongType)
	}

	missing := evalMetricRule(t, newMetricRequiredAttrs, func(m pmetric.Metric) { tokenUsageHistogram(m, "{token}", false) })
	if len(missing) != 1 || missing[0].Attribute != "gen_ai.token.type" {
		t.Errorf("missing gen_ai.token.type must be flagged, got %v", missing)
	}

	ok := evalMetricRule(t, newMetricRequiredAttrs, func(m pmetric.Metric) { tokenUsageHistogram(m, "{token}", true) })
	if len(ok) != 0 {
		t.Errorf("complete data point must pass, got %v", ok)
	}
}

func TestMetricSecondsSanity(t *testing.T) {
	looksLikeMillis := evalMetricRule(t, newMetricSecondsSanity, func(m pmetric.Metric) {
		m.SetName("gen_ai.client.operation.duration")
		m.SetUnit("s")
		dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
		dp.SetCount(2)
		dp.SetSum(9000)
		dp.SetMax(7500)
	})
	if len(looksLikeMillis) != 1 {
		t.Errorf("hour-plus second values must be flagged, got %v", looksLikeMillis)
	}

	sane := evalMetricRule(t, newMetricSecondsSanity, func(m pmetric.Metric) {
		m.SetName("gen_ai.client.operation.duration")
		m.SetUnit("s")
		dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
		dp.SetCount(3)
		dp.SetSum(4.5)
		dp.SetMax(2.1)
	})
	if len(sane) != 0 {
		t.Errorf("sane durations must pass, got %v", sane)
	}
}

func TestMetricUnknownNameAndBuckets(t *testing.T) {
	typo := evalMetricRule(t, newMetricUnknownName, func(m pmetric.Metric) {
		m.SetName("gen_ai.client.tokens.usage")
		m.SetUnit("{token}")
		m.SetEmptyHistogram().DataPoints().AppendEmpty()
	})
	if len(typo) != 1 {
		t.Errorf("unknown metric name must be flagged, got %v", typo)
	}

	custom := evalMetricRule(t, newMetricAdvisoryBuckets, func(m pmetric.Metric) {
		m.SetName("gen_ai.client.operation.duration")
		m.SetUnit("s")
		dp := m.SetEmptyHistogram().DataPoints().AppendEmpty()
		dp.ExplicitBounds().FromRaw([]float64{1, 10, 100})
	})
	if len(custom) != 1 || custom[0].Severity != engine.SeverityInfo {
		t.Errorf("custom buckets must be an INFO hint, got %v", custom)
	}
}

func evalLogRule(t *testing.T, factory func(*registry.Registry) engine.Rule, build func(lr plog.LogRecord)) []engine.Finding {
	t.Helper()
	reg := registry.Default()
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "log-test")
	lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	build(lr)
	eng := engine.New(reg, factory(reg))
	return eng.EvaluateLogs(ld).Findings
}

func TestEvaluationEventShapeAndAssociation(t *testing.T) {
	missingName := evalLogRule(t, newEventShape, func(lr plog.LogRecord) {
		lr.SetEventName("gen_ai.evaluation.result")
		lr.Attributes().PutDouble("gen_ai.evaluation.score.value", 0.9)
	})
	if len(missingName) != 1 || missingName[0].Attribute != "gen_ai.evaluation.name" {
		t.Errorf("missing gen_ai.evaluation.name must be flagged, got %v", missingName)
	}

	unassociated := evalLogRule(t, newEventEvaluationAssociation, func(lr plog.LogRecord) {
		lr.SetEventName("gen_ai.evaluation.result")
		lr.Attributes().PutStr("gen_ai.evaluation.name", "relevance")
	})
	if len(unassociated) != 1 {
		t.Errorf("unassociated evaluation must be flagged, got %v", unassociated)
	}

	associated := evalLogRule(t, newEventEvaluationAssociation, func(lr plog.LogRecord) {
		lr.SetEventName("gen_ai.evaluation.result")
		lr.Attributes().PutStr("gen_ai.evaluation.name", "relevance")
		lr.Attributes().PutStr("gen_ai.response.id", "chatcmpl-1")
	})
	if len(associated) != 0 {
		t.Errorf("response.id association must pass, got %v", associated)
	}
}

func TestExceptionSeverityAndUnknownEvents(t *testing.T) {
	wrongSeverity := evalLogRule(t, newEventExceptionSeverity, func(lr plog.LogRecord) {
		lr.SetEventName("gen_ai.client.operation.exception")
		lr.SetSeverityNumber(plog.SeverityNumberError)
		lr.Attributes().PutStr("exception.type", "Timeout")
	})
	if len(wrongSeverity) != 1 {
		t.Errorf("non-WARN exception severity must be flagged, got %v", wrongSeverity)
	}

	unknown := evalLogRule(t, newEventUnknownName, func(lr plog.LogRecord) {
		lr.SetEventName("gen_ai.evaluation.results")
		lr.Attributes().PutStr("gen_ai.evaluation.name", "relevance")
	})
	if len(unknown) != 1 {
		t.Errorf("unknown event name must be flagged, got %v", unknown)
	}
}
