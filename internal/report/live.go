package report

import (
	"sync"
	"time"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// exemplarsPerRule bounds how many concrete findings are retained per rule.
// The report stays useful (here are five real offending spans) without
// growing unbounded on a firehose of identical violations.
const exemplarsPerRule = 5

// RuleStatus is one catalog rule with its live violation count. Zero-count
// rules are included so the report shows what is being checked, not only
// what failed.
type RuleStatus struct {
	RuleID    string           `json:"rule_id"`
	Severity  engine.Severity  `json:"severity"`
	Brief     string           `json:"brief"`
	DocURL    string           `json:"doc_url"`
	Count     int              `json:"count"`
	Exemplars []engine.Finding `json:"exemplars,omitempty"`
}

// LiveReport is the gateway's /api/report payload.
type LiveReport struct {
	Tool        string              `json:"tool"`
	Version     string              `json:"version"`
	Semconv     Semconv             `json:"semconv"`
	StartedAt   time.Time           `json:"started_at"`
	GeneratedAt time.Time           `json:"generated_at"`
	Summary     Summary             `json:"summary"`
	Services    map[string]*Summary `json:"services"`
	Rules       []RuleStatus        `json:"rules"`
}

// Live aggregates engine results for the lifetime of the gateway process:
// cumulative per-service tallies plus per-rule counts with bounded
// exemplars. All methods are safe for concurrent use.
type Live struct {
	version string
	semconv Semconv
	catalog []engine.Rule

	mu        sync.RWMutex
	startedAt time.Time
	tallies   map[string]*engine.Tally
	counts    map[string]*severityCounts // by service
	rules     map[string]*ruleAgg        // by rule ID
}

type severityCounts struct{ errors, warnings, infos int }

type ruleAgg struct {
	count     int
	exemplars []engine.Finding
}

// NewLive builds the live store for a rule catalog.
func NewLive(reg *registry.Registry, catalog []engine.Rule, version string) *Live {
	return &Live{
		version:   version,
		semconv:   Semconv{Repo: reg.Meta.Repo, SHA: reg.Meta.SHA, Date: reg.Meta.Date},
		catalog:   catalog,
		startedAt: time.Now(),
		tallies:   map[string]*engine.Tally{},
		counts:    map[string]*severityCounts{},
		rules:     map[string]*ruleAgg{},
	}
}

// Add folds one evaluation result into the aggregate.
func (l *Live) Add(res *engine.Result) {
	if res == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	for svc, t := range res.Tallies {
		mine, ok := l.tallies[svc]
		if !ok {
			mine = &engine.Tally{}
			l.tallies[svc] = mine
		}
		mine.Checks += t.Checks
		mine.Passed += t.Passed
	}
	for _, f := range res.Findings {
		sc, ok := l.counts[f.Service]
		if !ok {
			sc = &severityCounts{}
			l.counts[f.Service] = sc
		}
		switch f.Severity {
		case engine.SeverityError:
			sc.errors++
		case engine.SeverityWarning:
			sc.warnings++
		case engine.SeverityInfo:
			sc.infos++
		}
		agg, ok := l.rules[f.RuleID]
		if !ok {
			agg = &ruleAgg{}
			l.rules[f.RuleID] = agg
		}
		agg.count++
		if len(agg.exemplars) < exemplarsPerRule {
			agg.exemplars = append(agg.exemplars, f)
		}
	}
}

// Reset clears all aggregates (used by demos and e2e phases).
func (l *Live) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.startedAt = time.Now()
	l.tallies = map[string]*engine.Tally{}
	l.counts = map[string]*severityCounts{}
	l.rules = map[string]*ruleAgg{}
}

// Snapshot renders the current aggregate as a LiveReport.
func (l *Live) Snapshot() *LiveReport {
	l.mu.RLock()
	defer l.mu.RUnlock()

	rep := &LiveReport{
		Tool:        "genai-conformance",
		Version:     l.version,
		Semconv:     l.semconv,
		StartedAt:   l.startedAt,
		GeneratedAt: time.Now(),
		Services:    map[string]*Summary{},
	}
	for svc, t := range l.tallies {
		rep.Services[svc] = &Summary{Score: t.Score(), Checks: t.Checks, Passed: t.Passed}
	}
	for svc, sc := range l.counts {
		s, ok := rep.Services[svc]
		if !ok {
			s = &Summary{Score: 1}
			rep.Services[svc] = s
		}
		s.Errors, s.Warnings, s.Infos = sc.errors, sc.warnings, sc.infos
	}
	for _, s := range rep.Services {
		rep.Summary.Checks += s.Checks
		rep.Summary.Passed += s.Passed
		rep.Summary.Errors += s.Errors
		rep.Summary.Warnings += s.Warnings
		rep.Summary.Infos += s.Infos
	}
	rep.Summary.Score = engine.Tally{Checks: rep.Summary.Checks, Passed: rep.Summary.Passed}.Score()

	rep.Rules = make([]RuleStatus, 0, len(l.catalog))
	for _, rule := range l.catalog { // catalog is already sorted by ID
		status := RuleStatus{
			RuleID:   rule.ID(),
			Severity: rule.Severity(),
			Brief:    rule.Brief(),
			DocURL:   rule.DocURL(),
		}
		if agg, ok := l.rules[rule.ID()]; ok {
			status.Count = agg.count
			status.Exemplars = append([]engine.Finding(nil), agg.exemplars...)
		}
		rep.Rules = append(rep.Rules, status)
	}
	return rep
}

// ServiceScores returns the current per-service scores (for the score gauge
// and the badge endpoint).
func (l *Live) ServiceScores() map[string]float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]float64, len(l.tallies))
	for svc, t := range l.tallies {
		out[svc] = t.Score()
	}
	return out
}
