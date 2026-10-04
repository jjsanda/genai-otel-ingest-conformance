package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// goldenReport is the stable projection of a validate run asserted by the
// fixture goldens: exit code, score arithmetic, and the exact finding set
// (rule, severity, attribute) — but not message wording, which may improve
// without being a breaking change.
type goldenReport struct {
	ExitCode int             `json:"exit_code"`
	Score    float64         `json:"score"`
	Checks   int             `json:"checks"`
	Passed   int             `json:"passed"`
	Findings []goldenFinding `json:"findings"`
}

type goldenFinding struct {
	RuleID    string `json:"rule_id"`
	Severity  string `json:"severity"`
	Attribute string `json:"attribute,omitempty"`
}

func runValidateJSON(t *testing.T, path string) goldenReport {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main([]string{"validate", "--format", "json", path}, &out, &errOut)
	if code == ExitUsage {
		t.Fatalf("validate failed: %s", errOut.String())
	}
	var rep struct {
		Summary struct {
			Score  float64 `json:"score"`
			Checks int     `json:"checks"`
			Passed int     `json:"passed"`
		} `json:"summary"`
		Findings []goldenFinding `json:"findings"`
	}
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, out.String())
	}
	got := goldenReport{
		ExitCode: code,
		Score:    rep.Summary.Score,
		Checks:   rep.Summary.Checks,
		Passed:   rep.Summary.Passed,
		Findings: rep.Findings,
	}
	if got.Findings == nil {
		got.Findings = []goldenFinding{}
	}
	return got
}

func TestValidateFixturesAgainstGoldens(t *testing.T) {
	fixtures, err := filepath.Glob("../../testdata/fixtures/*/*/*.json")
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	for _, f := range fixtures {
		if !strings.HasSuffix(f, ".golden.json") {
			inputs = append(inputs, f)
		}
	}
	if len(inputs) < 8 {
		t.Fatalf("expected at least 8 fixtures, found %d", len(inputs))
	}

	for _, fixture := range inputs {
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			got := runValidateJSON(t, fixture)

			// valid/ fixtures are exemplary: no findings at all. invalid/
			// fixtures must produce findings; whether they gate the build
			// (exit 1) depends on severity and is captured by the golden.
			if strings.Contains(fixture, "/valid/") && (got.ExitCode != ExitOK || len(got.Findings) != 0) {
				t.Errorf("valid fixture must exit 0 with no findings, got exit %d, findings %v", got.ExitCode, got.Findings)
			}
			if strings.Contains(fixture, "/invalid/") && len(got.Findings) == 0 {
				t.Error("invalid fixture produced no findings")
			}

			goldenPath := strings.TrimSuffix(fixture, ".json") + ".golden.json"
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				raw, err := json.MarshalIndent(got, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, append(raw, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			raw, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("golden missing (run UPDATE_GOLDEN=1 go test ./internal/cli): %v", err)
			}
			var want goldenReport
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.MarshalIndent(got, "", "  ")
				t.Errorf("report drifted from golden %s.\ngot:\n%s\nwant:\n%s", filepath.Base(goldenPath), gotJSON, raw)
			}
		})
	}
}

func TestValidateValidFixtureIsPerfect(t *testing.T) {
	got := runValidateJSON(t, "../../testdata/fixtures/traces/valid/chat-agent.json")
	if got.Score != 1 || len(got.Findings) != 0 {
		t.Errorf("exemplary fixture must score 1.0 with zero findings, got score=%v findings=%v", got.Score, got.Findings)
	}
}

func TestValidateStrictFailsOnWarnings(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"validate", "--format", "json", "--strict",
		"../../testdata/fixtures/traces/invalid/unknown-service.json"}, &out, &errOut)
	if code != ExitFindings {
		t.Errorf("strict mode must fail on findings, got exit %d", code)
	}
}

func TestValidateJUnitIsWellFormed(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"validate", "--format", "junit",
		"../../testdata/fixtures/traces/invalid/bad-values.json"}, &out, &errOut)
	if code != ExitFindings {
		t.Fatalf("exit = %d, want %d", code, ExitFindings)
	}
	var suites struct {
		XMLName  xml.Name `xml:"testsuites"`
		Failures int      `xml:"failures,attr"`
	}
	if err := xml.Unmarshal(out.Bytes(), &suites); err != nil {
		t.Fatalf("junit output is not well-formed XML: %v\n%s", err, out.String())
	}
	if suites.Failures == 0 {
		t.Error("junit output must report failures for a broken fixture")
	}
}

func TestValidateMarkdownMentionsScoreAndPin(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"validate", "../../testdata/fixtures/traces/valid/chat-agent.json"}, &out, &errOut)
	if code != ExitOK {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"Score: 100.0%", "Semconv pin", "No findings"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown output missing %q:\n%s", want, text)
		}
	}
}

func TestValidateUsageErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main([]string{"validate"}, &out, &errOut); code != ExitUsage {
		t.Errorf("no files must exit 2, got %d", code)
	}
	if code := Main([]string{"validate", "--format", "yaml", "x.json"}, &out, &errOut); code != ExitUsage {
		t.Errorf("unknown format must exit 2, got %d", code)
	}
	if code := Main([]string{"validate", "does-not-exist.json"}, &out, &errOut); code != ExitUsage {
		t.Errorf("missing file must exit 2, got %d", code)
	}
}
