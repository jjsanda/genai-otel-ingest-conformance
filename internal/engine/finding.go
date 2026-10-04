// Package engine evaluates OpenTelemetry telemetry against the conformance
// rule catalog and aggregates findings into scoreable results. It is a plain
// library shared by the live gateway (serve) and the offline CI gate
// (validate); see ADR-0001.
package engine

// Severity classifies a finding, mapped from semconv requirement language:
// MUST / Required / Conditionally Required (condition met) violations are
// ERROR, SHOULD / Recommended violations are WARNING, and hints about opt-in
// or unusual-but-legal telemetry are INFO.
type Severity string

// Severity levels, ordered ERROR > WARNING > INFO.
const (
	SeverityError   Severity = "ERROR"
	SeverityWarning Severity = "WARNING"
	SeverityInfo    Severity = "INFO"
)

// Rank orders severities for sorting; higher is more severe.
func (s Severity) Rank() int {
	switch s {
	case SeverityError:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Signal identifies which telemetry signal a finding came from.
type Signal string

// Telemetry signals.
const (
	SignalTrace  Signal = "trace"
	SignalMetric Signal = "metric"
	SignalLog    Signal = "log"
)

// Finding is one conformance violation or hint. Content-attribute values are
// never copied into findings; only attribute names and structural facts are.
type Finding struct {
	RuleID      string   `json:"rule_id"`
	Severity    Severity `json:"severity"`
	Signal      Signal   `json:"signal"`
	Service     string   `json:"service"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
	DocURL      string   `json:"doc_url,omitempty"`

	// Location of the offending entity, populated per signal.
	TraceID   string `json:"trace_id,omitempty"`
	SpanID    string `json:"span_id,omitempty"`
	SpanName  string `json:"span_name,omitempty"`
	Metric    string `json:"metric,omitempty"`
	EventName string `json:"event_name,omitempty"`
	Attribute string `json:"attribute,omitempty"`
}

// Tally counts scoreable rule evaluations for one service. One evaluation is
// one non-INFO rule applied to one applicable entity (span, resource, metric,
// log record, or assembled trace); it passes when the rule reports nothing.
type Tally struct {
	Checks int `json:"checks"`
	Passed int `json:"passed"`
}

// Score is passed/checks in [0,1]; an empty tally scores 1 (nothing to fail).
func (t Tally) Score() float64 {
	if t.Checks == 0 {
		return 1
	}
	return float64(t.Passed) / float64(t.Checks)
}

// Result accumulates findings and per-service tallies across evaluations.
type Result struct {
	Findings []Finding
	Tallies  map[string]*Tally
}

// NewResult returns an empty result.
func NewResult() *Result {
	return &Result{Tallies: map[string]*Tally{}}
}

// record stores one rule evaluation outcome. INFO rules contribute findings
// but never affect the score.
func (r *Result) record(service string, sev Severity, findings []Finding) {
	r.Findings = append(r.Findings, findings...)
	if sev == SeverityInfo {
		return
	}
	t, ok := r.Tallies[service]
	if !ok {
		t = &Tally{}
		r.Tallies[service] = t
	}
	t.Checks++
	if len(findings) == 0 {
		t.Passed++
	}
}

// Merge folds another result into this one.
func (r *Result) Merge(o *Result) {
	if o == nil {
		return
	}
	r.Findings = append(r.Findings, o.Findings...)
	for svc, t := range o.Tallies {
		mine, ok := r.Tallies[svc]
		if !ok {
			mine = &Tally{}
			r.Tallies[svc] = mine
		}
		mine.Checks += t.Checks
		mine.Passed += t.Passed
	}
}

// Total sums tallies across services.
func (r *Result) Total() Tally {
	var out Tally
	for _, t := range r.Tallies {
		out.Checks += t.Checks
		out.Passed += t.Passed
	}
	return out
}

// Count returns the number of findings at the given severity.
func (r *Result) Count(sev Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// HasBlocking reports whether any ERROR finding exists.
func (r *Result) HasBlocking() bool {
	return r.Count(SeverityError) > 0
}
