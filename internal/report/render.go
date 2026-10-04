package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
)

// WriteJSON renders the report as indented JSON (docs/findings-schema.json).
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteMarkdown renders the report for humans: terminal output, PR comments,
// and GitHub step summaries.
func WriteMarkdown(w io.Writer, r *Report) error {
	fmt.Fprintf(w, "# GenAI telemetry conformance report\n\n")
	fmt.Fprintf(w, "**Score: %.1f%%** — %d/%d checks passed · %d errors · %d warnings · %d hints\n\n",
		r.Summary.Score*100, r.Summary.Passed, r.Summary.Checks,
		r.Summary.Errors, r.Summary.Warnings, r.Summary.Infos)
	fmt.Fprintf(w, "Semconv pin: [`%s`](%s/tree/%s) · tool %s\n\n", shortSHA(r.Semconv.SHA), r.Semconv.Repo, r.Semconv.SHA, r.Version)

	if len(r.Services) > 0 {
		fmt.Fprintf(w, "| Service | Score | Checks | Errors | Warnings | Hints |\n")
		fmt.Fprintf(w, "|---|---:|---:|---:|---:|---:|\n")
		for _, name := range r.ServiceNames() {
			s := r.Services[name]
			fmt.Fprintf(w, "| %s | %.1f%% | %d | %d | %d | %d |\n", name, s.Score*100, s.Checks, s.Errors, s.Warnings, s.Infos)
		}
		fmt.Fprintln(w)
	}

	if len(r.Findings) == 0 {
		fmt.Fprintf(w, "✅ No findings — the telemetry conforms to the pinned GenAI semantic conventions.\n")
		return nil
	}

	fmt.Fprintf(w, "## Findings\n\n")
	fmt.Fprintf(w, "| Severity | Rule | Service | Where | Problem | Fix | Seen |\n")
	fmt.Fprintf(w, "|---|---|---|---|---|---|---:|\n")
	// Identical findings across many entities (same rule, message, and
	// location description) collapse into one row with a count — sixteen
	// copies of "provider 'mock' is not well-known" teach nothing eleven
	// lines that one row with ×16 doesn't.
	type row struct {
		f     engine.Finding
		count int
	}
	var rows []*row
	index := map[string]*row{}
	for _, f := range r.Findings {
		key := string(f.Severity) + "|" + f.RuleID + "|" + f.Service + "|" + where(f) + "|" + f.Message
		if existing, ok := index[key]; ok {
			existing.count++
			continue
		}
		next := &row{f: f, count: 1}
		index[key] = next
		rows = append(rows, next)
	}
	for _, row := range rows {
		seen := ""
		if row.count > 1 {
			seen = fmt.Sprintf("×%d", row.count)
		}
		f := row.f
		fmt.Fprintf(w, "| %s | [%s](%s) | %s | %s | %s | %s | %s |\n",
			severityBadge(f.Severity), f.RuleID, f.DocURL, f.Service, escapePipes(where(f)), escapePipes(f.Message), escapePipes(f.Remediation), seen)
	}
	return nil
}

func where(f engine.Finding) string {
	switch {
	case f.SpanName != "":
		return fmt.Sprintf("span `%s`", f.SpanName)
	case f.Metric != "":
		return fmt.Sprintf("metric `%s`", f.Metric)
	case f.EventName != "":
		return fmt.Sprintf("event `%s`", f.EventName)
	case f.TraceID != "":
		return fmt.Sprintf("trace `%s`", shortSHA(f.TraceID))
	default:
		return "resource"
	}
}

func severityBadge(s engine.Severity) string {
	switch s {
	case engine.SeverityError:
		return "🔴 ERROR"
	case engine.SeverityWarning:
		return "🟡 WARNING"
	default:
		return "🔵 INFO"
	}
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func escapePipes(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '|' {
			out = append(out, '\\')
		}
		if r == '\n' {
			out = append(out, ' ')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// JUnit rendering: one testsuite per service, one testcase per (service,
// scoreable rule). A rule with findings becomes a failed testcase whose
// message aggregates the individual findings. INFO findings are hints, not
// gate results, and are deliberately omitted here — the JSON report carries
// them.
type junitTestsuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Suites   []junitTestsuite `xml:"testsuite"`
}

type junitTestsuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// WriteJUnit renders the report as JUnit XML for CI checks UIs.
func WriteJUnit(w io.Writer, r *Report) error {
	byServiceRule := map[string]map[string][]engine.Finding{}
	ruleIDs := map[string]bool{}
	for _, f := range r.Findings {
		if f.Severity == engine.SeverityInfo {
			continue
		}
		if byServiceRule[f.Service] == nil {
			byServiceRule[f.Service] = map[string][]engine.Finding{}
		}
		byServiceRule[f.Service][f.RuleID] = append(byServiceRule[f.Service][f.RuleID], f)
		ruleIDs[f.RuleID] = true
	}

	suites := junitTestsuites{}
	for _, svc := range r.ServiceNames() {
		suite := junitTestsuite{Name: "genai-conformance: " + svc}
		failing := byServiceRule[svc]
		var ids []string
		for id := range failing {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			findings := failing[id]
			body := ""
			for _, f := range findings {
				body += fmt.Sprintf("[%s] %s (%s)\n", f.Severity, f.Message, where(f))
			}
			suite.Cases = append(suite.Cases, junitTestcase{
				Name:      id,
				Classname: svc,
				Failure: &junitFailure{
					Message: fmt.Sprintf("%d finding(s) for %s", len(findings), id),
					Body:    body,
				},
			})
		}
		// One synthetic passing case keeps suites with zero findings visible.
		if len(suite.Cases) == 0 {
			suite.Cases = append(suite.Cases, junitTestcase{Name: "conformant", Classname: svc})
		}
		suite.Tests = len(suite.Cases)
		suite.Failures = len(ids)
		suites.Tests += suite.Tests
		suites.Failures += suite.Failures
		suites.Suites = append(suites.Suites, suite)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(suites); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
