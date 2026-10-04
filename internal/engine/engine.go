package engine

import (
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// Engine runs the rule catalog over telemetry batches.
type Engine struct {
	reg *registry.Registry

	spanRules     []SpanRule
	resourceRules []ResourceRule
	metricRules   []MetricRule
	logRules      []LogRule
	traceRules    []TraceRule
	all           []Rule
}

// New builds an engine from the registry and a rule catalog. It panics on a
// rule that implements none of the Check interfaces — that is a programming
// error caught by any test.
func New(reg *registry.Registry, rules ...Rule) *Engine {
	e := &Engine{reg: reg}
	for _, r := range rules {
		matched := false
		if sr, ok := r.(SpanRule); ok {
			e.spanRules = append(e.spanRules, sr)
			matched = true
		}
		if rr, ok := r.(ResourceRule); ok {
			e.resourceRules = append(e.resourceRules, rr)
			matched = true
		}
		if mr, ok := r.(MetricRule); ok {
			e.metricRules = append(e.metricRules, mr)
			matched = true
		}
		if lr, ok := r.(LogRule); ok {
			e.logRules = append(e.logRules, lr)
			matched = true
		}
		if tr, ok := r.(TraceRule); ok {
			e.traceRules = append(e.traceRules, tr)
			matched = true
		}
		if !matched {
			panic(fmt.Sprintf("engine: rule %s implements no Check interface", r.ID()))
		}
		e.all = append(e.all, r)
	}
	sort.Slice(e.all, func(i, j int) bool { return e.all[i].ID() < e.all[j].ID() })
	return e
}

// Registry exposes the registry the engine was built with.
func (e *Engine) Registry() *registry.Registry { return e.reg }

// Rules returns the catalog sorted by rule ID.
func (e *Engine) Rules() []Rule { return e.all }

// EvaluateTraces runs span and resource rules over a trace batch. Trace-level
// topology rules run separately over assembled traces (EvaluateTrace).
func (e *Engine) EvaluateTraces(td ptrace.Traces) *Result {
	res := NewResult()
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		service := serviceName(rs.Resource())
		hasGenAI := false
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				sctx := NewSpanContext(e.reg, rs.Resource(), ss.Scope(), ss.Spans().At(k), service)
				if !sctx.IsGenAI() {
					continue
				}
				hasGenAI = true
				for _, rule := range e.spanRules {
					res.record(service, rule.Severity(), rule.CheckSpan(sctx))
				}
			}
		}
		if hasGenAI {
			rctx := &ResourceContext{Registry: e.reg, Resource: rs.Resource(), Signal: SignalTrace, Service: service}
			for _, rule := range e.resourceRules {
				res.record(service, rule.Severity(), rule.CheckResource(rctx))
			}
		}
	}
	return res
}

// EvaluateMetrics runs metric and resource rules over a metric batch.
func (e *Engine) EvaluateMetrics(md pmetric.Metrics) *Result {
	res := NewResult()
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		service := serviceName(rm.Resource())
		hasGenAI := false
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			for k := 0; k < sm.Metrics().Len(); k++ {
				metric := sm.Metrics().At(k)
				if !e.isGenAIMetric(metric.Name()) {
					continue
				}
				hasGenAI = true
				mctx := &MetricContext{
					Registry: e.reg,
					Resource: rm.Resource(),
					Scope:    sm.Scope(),
					Metric:   metric,
					Service:  service,
				}
				if shape, ok := e.reg.Metrics[metric.Name()]; ok {
					mctx.shape = shape
				}
				for _, rule := range e.metricRules {
					res.record(service, rule.Severity(), rule.CheckMetric(mctx))
				}
			}
		}
		if hasGenAI {
			rctx := &ResourceContext{Registry: e.reg, Resource: rm.Resource(), Signal: SignalMetric, Service: service}
			for _, rule := range e.resourceRules {
				res.record(service, rule.Severity(), rule.CheckResource(rctx))
			}
		}
	}
	return res
}

// EvaluateLogs runs log and resource rules over a log batch.
func (e *Engine) EvaluateLogs(ld plog.Logs) *Result {
	res := NewResult()
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		rl := ld.ResourceLogs().At(i)
		service := serviceName(rl.Resource())
		hasGenAI := false
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			for k := 0; k < sl.LogRecords().Len(); k++ {
				record := sl.LogRecords().At(k)
				if !isGenAILog(record) {
					continue
				}
				hasGenAI = true
				lctx := &LogContext{
					Registry: e.reg,
					Resource: rl.Resource(),
					Scope:    sl.Scope(),
					Record:   record,
					Service:  service,
				}
				if shape, ok := e.reg.Events[record.EventName()]; ok {
					lctx.event = shape
				}
				for _, rule := range e.logRules {
					res.record(service, rule.Severity(), rule.CheckLog(lctx))
				}
			}
		}
		if hasGenAI {
			rctx := &ResourceContext{Registry: e.reg, Resource: rl.Resource(), Signal: SignalLog, Service: service}
			for _, rule := range e.resourceRules {
				res.record(service, rule.Severity(), rule.CheckResource(rctx))
			}
		}
	}
	return res
}

// EvaluateTrace runs trace-topology rules over one assembled trace. The
// caller (the gateway's window store, or validate's offline assembler)
// decides when a trace is complete enough to judge.
func (e *Engine) EvaluateTrace(tc *TraceContext) *Result {
	res := NewResult()
	if tc == nil || len(tc.Spans) == 0 {
		return res
	}
	hasGenAI := false
	for _, s := range tc.Spans {
		if s.IsGenAI() {
			hasGenAI = true
			break
		}
	}
	if !hasGenAI {
		return res
	}
	service := tc.GenAIService()
	for _, rule := range e.traceRules {
		if a, ok := rule.(TraceApplicable); ok && !a.AppliesToTrace(tc) {
			continue // not applicable to this trace: no check, no finding
		}
		res.record(service, rule.Severity(), rule.CheckTrace(tc))
	}
	return res
}

// AssembleTraces groups every span in the given batches by trace ID — the
// offline equivalent of the gateway's trace windows, used by validate where
// the input files are the complete universe of spans.
func (e *Engine) AssembleTraces(batches []ptrace.Traces) []*TraceContext {
	byTrace := map[string]*TraceContext{}
	var order []string
	for _, td := range batches {
		for i := 0; i < td.ResourceSpans().Len(); i++ {
			rs := td.ResourceSpans().At(i)
			service := serviceName(rs.Resource())
			for j := 0; j < rs.ScopeSpans().Len(); j++ {
				ss := rs.ScopeSpans().At(j)
				for k := 0; k < ss.Spans().Len(); k++ {
					span := ss.Spans().At(k)
					// Spans without a trace ID cannot be assembled; keying
					// on the empty string "" would merge every zero-ID span
					// across services into one bogus trace (span/resource
					// rules still judged them). Skip them here.
					if span.TraceID().IsEmpty() {
						continue
					}
					id := span.TraceID().String()
					tc, ok := byTrace[id]
					if !ok {
						tc = &TraceContext{Registry: e.reg, TraceID: id, ByID: map[string]*SpanContext{}}
						byTrace[id] = tc
						order = append(order, id)
					}
					sctx := NewSpanContext(e.reg, rs.Resource(), ss.Scope(), span, service)
					tc.Spans = append(tc.Spans, sctx)
					tc.ByID[span.SpanID().String()] = sctx
				}
			}
		}
	}
	out := make([]*TraceContext, 0, len(order))
	for _, id := range order {
		out = append(out, byTrace[id])
	}
	return out
}

func (e *Engine) isGenAIMetric(name string) bool {
	if _, ok := e.reg.Metrics[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "gen_ai.")
}

func isGenAILog(record plog.LogRecord) bool {
	if registry.IsGenAI(record.EventName()) {
		return true
	}
	genAI := false
	record.Attributes().Range(func(k string, _ pcommon.Value) bool {
		if registry.IsGenAI(k) {
			genAI = true
			return false
		}
		return true
	})
	return genAI
}
