package engine

import (
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// SpanContext hands a rule one span plus its resource scope and the
// pre-resolved registry operation.
type SpanContext struct {
	Registry *registry.Registry
	Resource pcommon.Resource
	Scope    pcommon.InstrumentationScope
	Span     ptrace.Span
	Service  string

	genAI  bool
	opName string
	op     *registry.Operation
}

// NewSpanContext builds the evaluation context for one span. It is exported
// for the gateway's trace-window store, which assembles TraceContexts from
// spans arriving across batches.
func NewSpanContext(reg *registry.Registry, res pcommon.Resource, scope pcommon.InstrumentationScope, span ptrace.Span, service string) *SpanContext {
	c := &SpanContext{Registry: reg, Resource: res, Scope: scope, Span: span, Service: service}
	span.Attributes().Range(func(k string, _ pcommon.Value) bool {
		if registry.IsGenAI(k) {
			c.genAI = true
			return false
		}
		return true
	})
	if !c.genAI {
		for i := 0; i < span.Events().Len(); i++ {
			if registry.IsGenAI(span.Events().At(i).Name()) {
				c.genAI = true
				break
			}
		}
	}
	if v, ok := span.Attributes().Get("gen_ai.operation.name"); ok && v.Type() == pcommon.ValueTypeStr {
		c.opName = v.Str()
		if op, found := reg.OperationForName(c.opName); found {
			c.op = op
		}
	}
	return c
}

// IsGenAI reports whether the span carries any gen_ai.* attribute or event.
func (c *SpanContext) IsGenAI() bool { return c.genAI }

// OperationName returns the gen_ai.operation.name value, or "".
func (c *SpanContext) OperationName() string { return c.opName }

// Operation returns the resolved operation matrix, if the operation name is
// present and known.
func (c *SpanContext) Operation() (*registry.Operation, bool) {
	return c.op, c.op != nil
}

// Attr fetches a span attribute.
func (c *SpanContext) Attr(key string) (pcommon.Value, bool) {
	return c.Span.Attributes().Get(key)
}

// HasAttr reports attribute presence.
func (c *SpanContext) HasAttr(key string) bool {
	_, ok := c.Span.Attributes().Get(key)
	return ok
}

// KindString returns the span kind in semconv-style lower case ("client").
func (c *SpanContext) KindString() string {
	return strings.ToLower(c.Span.Kind().String())
}

// NewFinding builds a finding anchored to this span.
func (c *SpanContext) NewFinding(r Rule, attribute, message, remediation string) Finding {
	return Finding{
		RuleID:      r.ID(),
		Severity:    r.Severity(),
		Signal:      SignalTrace,
		Service:     c.Service,
		Message:     message,
		Remediation: remediation,
		DocURL:      r.DocURL(),
		TraceID:     c.Span.TraceID().String(),
		SpanID:      c.Span.SpanID().String(),
		SpanName:    c.Span.Name(),
		Attribute:   attribute,
	}
}

// ResourceContext hands a rule the resource of a batch that contained GenAI
// telemetry.
type ResourceContext struct {
	Registry *registry.Registry
	Resource pcommon.Resource
	Signal   Signal
	Service  string
}

// Attr fetches a resource attribute.
func (c *ResourceContext) Attr(key string) (pcommon.Value, bool) {
	return c.Resource.Attributes().Get(key)
}

// NewFinding builds a finding anchored to this resource.
func (c *ResourceContext) NewFinding(r Rule, attribute, message, remediation string) Finding {
	return Finding{
		RuleID:      r.ID(),
		Severity:    r.Severity(),
		Signal:      c.Signal,
		Service:     c.Service,
		Message:     message,
		Remediation: remediation,
		DocURL:      r.DocURL(),
		Attribute:   attribute,
	}
}

// MetricContext hands a rule one GenAI metric.
type MetricContext struct {
	Registry *registry.Registry
	Resource pcommon.Resource
	Scope    pcommon.InstrumentationScope
	Metric   pmetric.Metric
	Service  string

	shape *registry.Metric // nil when the metric name is unknown
}

// Shape returns the registry entry for this metric name, if known.
func (c *MetricContext) Shape() (*registry.Metric, bool) { return c.shape, c.shape != nil }

// NewFinding builds a finding anchored to this metric.
func (c *MetricContext) NewFinding(r Rule, attribute, message, remediation string) Finding {
	return Finding{
		RuleID:      r.ID(),
		Severity:    r.Severity(),
		Signal:      SignalMetric,
		Service:     c.Service,
		Message:     message,
		Remediation: remediation,
		DocURL:      r.DocURL(),
		Metric:      c.Metric.Name(),
		Attribute:   attribute,
	}
}

// LogContext hands a rule one GenAI log record.
type LogContext struct {
	Registry *registry.Registry
	Resource pcommon.Resource
	Scope    pcommon.InstrumentationScope
	Record   plog.LogRecord
	Service  string

	event *registry.Event // nil when the event name is unknown
}

// EventShape returns the registry entry for the record's event_name, if any.
func (c *LogContext) EventShape() (*registry.Event, bool) { return c.event, c.event != nil }

// Attr fetches a log record attribute.
func (c *LogContext) Attr(key string) (pcommon.Value, bool) {
	return c.Record.Attributes().Get(key)
}

// NewFinding builds a finding anchored to this log record.
func (c *LogContext) NewFinding(r Rule, attribute, message, remediation string) Finding {
	return Finding{
		RuleID:      r.ID(),
		Severity:    r.Severity(),
		Signal:      SignalLog,
		Service:     c.Service,
		Message:     message,
		Remediation: remediation,
		DocURL:      r.DocURL(),
		TraceID:     traceIDString(c.Record.TraceID()),
		SpanID:      spanIDString(c.Record.SpanID()),
		EventName:   c.Record.EventName(),
		Attribute:   attribute,
	}
}

// TraceContext hands a rule one assembled trace: every span observed for a
// trace ID, GenAI or not, across resources.
type TraceContext struct {
	Registry *registry.Registry
	TraceID  string
	Spans    []*SpanContext
	// ByID indexes spans by hex span ID for parent lookups.
	ByID map[string]*SpanContext
	// Incomplete marks traces that were evaluated before all spans could
	// arrive (window eviction or span-cap truncation); topology rules soften
	// accordingly.
	Incomplete bool
	// LateArrival marks a window created by spans that arrived after their
	// trace's window had already closed and been judged — itself a
	// conformance signal (mis-tuned batching or export timeouts upstream).
	LateArrival bool
}

// GenAIService returns the service of the first GenAI span in the trace,
// which is where trace-level findings are attributed.
func (c *TraceContext) GenAIService() string {
	for _, s := range c.Spans {
		if s.IsGenAI() {
			return s.Service
		}
	}
	if len(c.Spans) > 0 {
		return c.Spans[0].Service
	}
	return unknownService
}

// NewFinding builds a finding anchored to a span within this trace; pass nil
// to anchor to the trace as a whole.
//
// A trace is judged as a unit and its check tally is recorded under the
// trace's GenAI service (see Engine.EvaluateTrace), so every trace finding
// is attributed to that same service — even one about a span from another
// service in the same trace. Splitting them would let a service show
// findings next to a perfect score (findings counted under it, checks under
// the trace owner). The offending span is still located via SpanID/SpanName.
func (c *TraceContext) NewFinding(r Rule, span *SpanContext, message, remediation string) Finding {
	f := Finding{
		RuleID:      r.ID(),
		Severity:    r.Severity(),
		Signal:      SignalTrace,
		Service:     c.GenAIService(),
		Message:     message,
		Remediation: remediation,
		DocURL:      r.DocURL(),
		TraceID:     c.TraceID,
	}
	if span != nil {
		f.SpanID = span.Span.SpanID().String()
		f.SpanName = span.Span.Name()
	}
	return f
}

const unknownService = "unknown_service"

func serviceName(res pcommon.Resource) string {
	if v, ok := res.Attributes().Get("service.name"); ok && v.Type() == pcommon.ValueTypeStr && v.Str() != "" {
		return v.Str()
	}
	return unknownService
}

func traceIDString(id pcommon.TraceID) string {
	if id.IsEmpty() {
		return ""
	}
	return id.String()
}

func spanIDString(id pcommon.SpanID) string {
	if id.IsEmpty() {
		return ""
	}
	return id.String()
}
