// Package report turns engine results into consumable artifacts: JSON for
// tooling, Markdown for humans and CI summaries, JUnit XML for CI checks
// UIs, and (in serve mode) the live report API.
package report

import (
	"sort"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// Summary aggregates scoreable checks and finding counts.
type Summary struct {
	// Score is passed/checks over ERROR- and WARNING-level rule evaluations,
	// in [0,1]. INFO findings never affect it.
	Score    float64 `json:"score"`
	Checks   int     `json:"checks"`
	Passed   int     `json:"passed"`
	Errors   int     `json:"errors"`
	Warnings int     `json:"warnings"`
	Infos    int     `json:"infos"`
}

// Semconv identifies the conventions snapshot findings refer to.
type Semconv struct {
	Repo string `json:"repo"`
	SHA  string `json:"sha"`
	Date string `json:"date"`
}

// Report is the complete conformance report. Its JSON form is documented in
// docs/findings-schema.json and is part of the CI-gate contract.
type Report struct {
	Tool     string              `json:"tool"`
	Version  string              `json:"version"`
	Semconv  Semconv             `json:"semconv"`
	Summary  Summary             `json:"summary"`
	Services map[string]*Summary `json:"services"`
	Findings []engine.Finding    `json:"findings"`
}

// Build assembles a deterministic report from engine results: findings are
// sorted by severity, rule, service, and location so identical telemetry
// yields byte-identical reports (golden-test friendly).
func Build(res *engine.Result, reg *registry.Registry, version string) *Report {
	rep := &Report{
		Tool:     "genai-conformance",
		Version:  version,
		Semconv:  Semconv{Repo: reg.Meta.Repo, SHA: reg.Meta.SHA, Date: reg.Meta.Date},
		Services: map[string]*Summary{},
		Findings: append([]engine.Finding(nil), res.Findings...),
	}

	for svc, tally := range res.Tallies {
		rep.Services[svc] = &Summary{Checks: tally.Checks, Passed: tally.Passed}
	}
	for _, f := range res.Findings {
		svc, ok := rep.Services[f.Service]
		if !ok {
			svc = &Summary{}
			rep.Services[f.Service] = svc
		}
		switch f.Severity {
		case engine.SeverityError:
			svc.Errors++
		case engine.SeverityWarning:
			svc.Warnings++
		case engine.SeverityInfo:
			svc.Infos++
		}
	}
	for _, svc := range rep.Services {
		svc.Score = engine.Tally{Checks: svc.Checks, Passed: svc.Passed}.Score()
		rep.Summary.Checks += svc.Checks
		rep.Summary.Passed += svc.Passed
		rep.Summary.Errors += svc.Errors
		rep.Summary.Warnings += svc.Warnings
		rep.Summary.Infos += svc.Infos
	}
	rep.Summary.Score = engine.Tally{Checks: rep.Summary.Checks, Passed: rep.Summary.Passed}.Score()

	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() > b.Severity.Rank()
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.TraceID != b.TraceID {
			return a.TraceID < b.TraceID
		}
		if a.SpanID != b.SpanID {
			return a.SpanID < b.SpanID
		}
		return a.Attribute < b.Attribute
	})
	return rep
}

// ServiceNames returns the report's services sorted by name.
func (r *Report) ServiceNames() []string {
	names := make([]string, 0, len(r.Services))
	for name := range r.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
