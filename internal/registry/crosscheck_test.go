package registry

// These tests keep the hand-curated registry honest against the vendored
// upstream model (third_party/semconv-genai/, pinned by hack/sync-semconv.sh).
// If an upstream attribute type, enum, metric shape, or event name is
// transcribed incorrectly, they fail. See ADR-0002.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const vendoredDir = "../../third_party/semconv-genai/model/gen-ai"

// upstreamType is either a plain scalar type ("string", "int", ...) or an
// enum ({members: [{value: ...}]}) in the weaver model.
type upstreamType struct {
	Scalar string
	Enum   []string
}

func (t *upstreamType) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		t.Scalar = node.Value
		return nil
	}
	var m struct {
		Members []struct {
			Value string `yaml:"value"`
		} `yaml:"members"`
	}
	if err := node.Decode(&m); err != nil {
		return err
	}
	t.Scalar = "enum"
	for _, member := range m.Members {
		t.Enum = append(t.Enum, member.Value)
	}
	return nil
}

func loadUpstreamAttributes(t *testing.T) map[string]upstreamType {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(vendoredDir, "registry.yaml"))
	if err != nil {
		t.Fatalf("vendored upstream model missing (run hack/sync-semconv.sh): %v", err)
	}
	var doc struct {
		Attributes []struct {
			Key  string       `yaml:"key"`
			Type upstreamType `yaml:"type"`
		} `yaml:"attributes"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing vendored registry.yaml: %v", err)
	}
	out := make(map[string]upstreamType, len(doc.Attributes))
	for _, a := range doc.Attributes {
		out[a.Key] = a.Type
	}
	return out
}

// weaverToCurated maps weaver scalar type names to curated AttrType values.
var weaverToCurated = map[string]AttrType{
	"string":           TypeString,
	"int":              TypeInt,
	"double":           TypeDouble,
	"boolean":          TypeBool,
	"string[]":         TypeStringArray,
	"any":              TypeAny,
	"template[string]": TypeTemplateString,
	"enum":             TypeString, // enums are strings on the wire
}

func TestCuratedAttributeTypesMatchUpstream(t *testing.T) {
	r := mustLoad(t)
	upstream := loadUpstreamAttributes(t)

	for key, attr := range r.Attributes {
		if !IsGenAI(key) {
			// error.type, server.*, exception.* live in the core semconv
			// registry, not in the vendored GenAI model.
			continue
		}
		up, ok := upstream[key]
		if !ok {
			t.Errorf("curated attribute %s not present upstream at the pin", key)
			continue
		}
		want, known := weaverToCurated[up.Scalar]
		if !known {
			t.Errorf("attribute %s: unhandled weaver type %q", key, up.Scalar)
			continue
		}
		if attr.Type != want {
			t.Errorf("attribute %s: curated type %s, upstream %s (maps to %s)", key, attr.Type, up.Scalar, want)
		}
		if len(up.Enum) > 0 {
			if !slices.Equal(sortedCopy(attr.Enum), sortedCopy(up.Enum)) {
				t.Errorf("attribute %s: curated enum %v differs from upstream %v", key, attr.Enum, up.Enum)
			}
		}
	}
}

func TestOperationNamesMatchUpstreamEnumExactly(t *testing.T) {
	r := mustLoad(t)
	upstream := loadUpstreamAttributes(t)

	up, ok := upstream["gen_ai.operation.name"]
	if !ok || len(up.Enum) == 0 {
		t.Fatal("upstream gen_ai.operation.name enum not found")
	}
	var curated []string
	for _, op := range r.Operations {
		curated = append(curated, op.OperationNames...)
	}
	if !slices.Equal(sortedCopy(curated), sortedCopy(up.Enum)) {
		t.Errorf("curated operation names %v\nupstream enum %v", sortedCopy(curated), sortedCopy(up.Enum))
	}
}

func TestMetricsMatchUpstream(t *testing.T) {
	r := mustLoad(t)
	raw, err := os.ReadFile(filepath.Join(vendoredDir, "metrics.yaml"))
	if err != nil {
		t.Fatalf("vendored metrics.yaml missing: %v", err)
	}
	var doc struct {
		Metrics []struct {
			Name       string `yaml:"name"`
			Instrument string `yaml:"instrument"`
			Unit       string `yaml:"unit"`
		} `yaml:"metrics"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing vendored metrics.yaml: %v", err)
	}
	upstream := map[string]struct{ instrument, unit string }{}
	for _, m := range doc.Metrics {
		upstream[m.Name] = struct{ instrument, unit string }{m.Instrument, m.Unit}
	}
	if len(upstream) != len(r.Metrics) {
		t.Errorf("curated has %d metrics, upstream %d", len(r.Metrics), len(upstream))
	}
	for name, m := range r.Metrics {
		up, ok := upstream[name]
		if !ok {
			t.Errorf("curated metric %s not present upstream", name)
			continue
		}
		if m.Instrument != up.instrument || m.Unit != up.unit {
			t.Errorf("metric %s: curated (%s, %s) vs upstream (%s, %s)", name, m.Instrument, m.Unit, up.instrument, up.unit)
		}
	}
}

func TestEventsMatchUpstream(t *testing.T) {
	r := mustLoad(t)
	raw, err := os.ReadFile(filepath.Join(vendoredDir, "events.yaml"))
	if err != nil {
		t.Fatalf("vendored events.yaml missing: %v", err)
	}
	var doc struct {
		Events []struct {
			Name       string `yaml:"name"`
			Attributes []struct {
				Ref              string    `yaml:"ref"`
				RequirementLevel yaml.Node `yaml:"requirement_level"`
			} `yaml:"attributes"`
		} `yaml:"events"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing vendored events.yaml: %v", err)
	}

	upstreamNames := make([]string, 0, len(doc.Events))
	for _, e := range doc.Events {
		upstreamNames = append(upstreamNames, e.Name)
	}
	var curatedNames []string
	for name := range r.Events {
		curatedNames = append(curatedNames, name)
	}
	if !slices.Equal(sortedCopy(curatedNames), sortedCopy(upstreamNames)) {
		t.Errorf("curated events %v differ from upstream %v", sortedCopy(curatedNames), sortedCopy(upstreamNames))
	}

	// Every upstream attribute marked plainly "required" on an event must be
	// required in the curated matrix too.
	for _, e := range doc.Events {
		curated, ok := r.Events[e.Name]
		if !ok {
			continue
		}
		for _, a := range e.Attributes {
			if a.Ref == "" || a.RequirementLevel.Kind != yaml.ScalarNode || a.RequirementLevel.Value != "required" {
				continue
			}
			if !slices.Contains(curated.Attributes.Required, a.Ref) {
				t.Errorf("event %s: upstream requires %s but curated does not", e.Name, a.Ref)
			}
		}
	}
}

func TestPinnedSHAMatchesVendored(t *testing.T) {
	r := mustLoad(t)
	raw, err := os.ReadFile("../../third_party/semconv-genai/PINNED_SHA")
	if err != nil {
		t.Fatalf("PINNED_SHA missing: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != r.Meta.SHA {
		t.Errorf("curated meta SHA %s != vendored PINNED_SHA %s", r.Meta.SHA, got)
	}
	if !strings.Contains(r.Meta.DocsBase, r.Meta.SHA) {
		t.Error("docs_base must be anchored to the pinned SHA so links never rot")
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
