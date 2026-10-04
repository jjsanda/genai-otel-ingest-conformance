// Package registry provides the curated GenAI semantic-conventions
// conformance registry: per-operation attribute requirement matrices, metric
// and event shapes, and deprecation mappings, all pinned to a specific
// upstream commit of open-telemetry/semantic-conventions-genai.
//
// The curated YAML under curated/ is hand-transcribed from the pinned
// upstream model (see ADR-0002); crosscheck tests keep it honest against the
// vendored copy in third_party/semconv-genai/.
package registry

import "strings"

// AttrType is the expected wire type of an attribute value.
type AttrType string

// Attribute wire types used by the curated registry.
const (
	TypeString         AttrType = "string"
	TypeInt            AttrType = "int"
	TypeDouble         AttrType = "double"
	TypeBool           AttrType = "boolean"
	TypeStringArray    AttrType = "string_array"
	TypeAny            AttrType = "any"
	TypeTemplateString AttrType = "template_string"
)

// Meta records the provenance of the curated registry.
type Meta struct {
	Repo               string `yaml:"repo"`
	SHA                string `yaml:"sha"`
	Date               string `yaml:"date"`
	CoreSemconvVersion string `yaml:"core_semconv_version"`
	DocsBase           string `yaml:"docs_base"`
}

// Attribute describes one attribute's expected shape.
type Attribute struct {
	Type     AttrType `yaml:"type"`
	Enum     []string `yaml:"enum"`
	EnumOpen bool     `yaml:"enum_open"`
	Min      *float64 `yaml:"min"`
	Content  bool     `yaml:"content"`
}

// Levels groups attribute keys by semconv requirement level.
type Levels struct {
	Required              []string          `yaml:"required"`
	ConditionallyRequired map[string]string `yaml:"conditionally_required"`
	Recommended           []string          `yaml:"recommended"`
	OptIn                 []string          `yaml:"opt_in"`
}

// Kinds lists permissible span kinds for an operation.
type Kinds struct {
	Allowed   []string `yaml:"allowed"`
	Preferred string   `yaml:"preferred"`
}

// OperationAttributes extends Levels with span-kind-specific requirements
// (e.g. invoke_agent requires gen_ai.provider.name only on CLIENT spans).
type OperationAttributes struct {
	Levels                        `yaml:",inline"`
	RequiredWhenKind              map[string][]string          `yaml:"required_when_kind"`
	ConditionallyRequiredWhenKind map[string]map[string]string `yaml:"conditionally_required_when_kind"`
	RecommendedWhenKind           map[string][]string          `yaml:"recommended_when_kind"`
}

// Operation is the conformance matrix for one GenAI operation family.
type Operation struct {
	Key            string              `yaml:"-"`
	Doc            string              `yaml:"doc"`
	OperationNames []string            `yaml:"operation_names"`
	Kinds          Kinds               `yaml:"kinds"`
	NameFormat     string              `yaml:"name_format"`
	Attributes     OperationAttributes `yaml:"attributes"`
}

// EffectiveLevels merges the base requirement levels with the levels specific
// to the given span kind (lower-case, e.g. "client").
func (o *Operation) EffectiveLevels(kind string) Levels {
	out := Levels{
		Required:              append([]string(nil), o.Attributes.Required...),
		Recommended:           append([]string(nil), o.Attributes.Recommended...),
		OptIn:                 append([]string(nil), o.Attributes.OptIn...),
		ConditionallyRequired: map[string]string{},
	}
	for k, v := range o.Attributes.ConditionallyRequired {
		out.ConditionallyRequired[k] = v
	}
	out.Required = append(out.Required, o.Attributes.RequiredWhenKind[kind]...)
	out.Recommended = append(out.Recommended, o.Attributes.RecommendedWhenKind[kind]...)
	for k, v := range o.Attributes.ConditionallyRequiredWhenKind[kind] {
		out.ConditionallyRequired[k] = v
	}
	return out
}

// Metric is the conformance matrix for one GenAI metric instrument.
type Metric struct {
	Name            string    `yaml:"-"`
	Doc             string    `yaml:"doc"`
	Instrument      string    `yaml:"instrument"`
	Unit            string    `yaml:"unit"`
	ValueType       string    `yaml:"value_type"`
	StreamingOnly   bool      `yaml:"streaming_only"`
	Attributes      Levels    `yaml:"attributes"`
	AdvisoryBuckets []float64 `yaml:"advisory_buckets"`
}

// Event is the conformance matrix for one GenAI event (a log record
// carrying an event_name).
type Event struct {
	Name           string `yaml:"-"`
	Doc            string `yaml:"doc"`
	Brief          string `yaml:"brief"`
	Association    string `yaml:"association"`
	SeverityNumber int    `yaml:"severity_number"`
	ContentOptIn   bool   `yaml:"content_opt_in"`
	Attributes     Levels `yaml:"attributes"`
}

// Deprecation maps a legacy telemetry shape to its replacement.
type Deprecation struct {
	Replacement string `yaml:"replacement"`
	Note        string `yaml:"note"`
}

// Registry is the fully loaded and validated conformance registry.
type Registry struct {
	Meta                 Meta
	Attributes           map[string]Attribute
	Operations           map[string]*Operation
	Metrics              map[string]*Metric
	Events               map[string]*Event
	DeprecatedAttributes map[string]Deprecation
	DeprecatedEvents     map[string]Deprecation

	opByName map[string]*Operation
}

// OperationForName resolves a gen_ai.operation.name value (e.g. "chat") to
// its operation family (e.g. the "inference" matrix).
func (r *Registry) OperationForName(name string) (*Operation, bool) {
	op, ok := r.opByName[name]
	return op, ok
}

// DocURL returns the pinned-SHA documentation URL for a doc path that is
// relative to the upstream docs/gen-ai/ directory.
func (r *Registry) DocURL(rel string) string {
	return r.Meta.DocsBase + rel
}

// IsGenAI reports whether the attribute key is in the gen_ai namespace.
func IsGenAI(attrKey string) bool {
	return strings.HasPrefix(attrKey, "gen_ai.")
}
