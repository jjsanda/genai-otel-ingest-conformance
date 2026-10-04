package engine

// Rule is the metadata surface shared by all conformance rules. Concrete
// rules additionally implement exactly one of the signal-specific Check
// interfaces below.
type Rule interface {
	// ID is the stable rule identifier, e.g. "GENAI-SPAN-001".
	ID() string
	// Severity is the rule's severity class (a rule reports at one level).
	Severity() Severity
	// Brief is a one-line description used in the generated rule catalog.
	Brief() string
	// DocURL links to the pinned semconv documentation backing the rule.
	DocURL() string
}

// SpanRule checks one GenAI span.
type SpanRule interface {
	Rule
	CheckSpan(ctx *SpanContext) []Finding
}

// ResourceRule checks the resource of a batch that contains GenAI telemetry.
type ResourceRule interface {
	Rule
	CheckResource(ctx *ResourceContext) []Finding
}

// MetricRule checks one GenAI metric.
type MetricRule interface {
	Rule
	CheckMetric(ctx *MetricContext) []Finding
}

// LogRule checks one GenAI log record (event).
type LogRule interface {
	Rule
	CheckLog(ctx *LogContext) []Finding
}

// TraceRule checks one assembled trace (topology, continuity, timing).
type TraceRule interface {
	Rule
	CheckTrace(ctx *TraceContext) []Finding
}

// TraceApplicable lets a trace rule declare that it does not apply to a given
// assembled trace (e.g. topology rules skip incomplete or late-arriving
// traces). The engine then records neither a check nor a finding — a skipped
// rule must never count as a passed check, which would inflate the score
// exactly when the gateway could judge the least. A trace rule that does not
// implement this always applies.
type TraceApplicable interface {
	AppliesToTrace(ctx *TraceContext) bool
}

// Base carries rule metadata; concrete rules embed it.
type Base struct {
	RuleID   string
	Sev      Severity
	RuleDoc  string
	RuleNote string
}

// ID implements Rule.
func (b Base) ID() string { return b.RuleID }

// Severity implements Rule.
func (b Base) Severity() Severity { return b.Sev }

// Brief implements Rule.
func (b Base) Brief() string { return b.RuleNote }

// DocURL implements Rule.
func (b Base) DocURL() string { return b.RuleDoc }
