package registry

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed curated/*.yaml
var curatedFS embed.FS

var placeholderRE = regexp.MustCompile(`\{([^{}]+)\}`)

var spanKindNames = map[string]bool{
	"client": true, "server": true, "internal": true, "producer": true, "consumer": true,
}

var (
	defaultOnce sync.Once
	defaultReg  *Registry
	defaultErr  error
)

// Default returns the process-wide registry loaded from the embedded curated
// YAML. It panics if the embedded data is invalid, which Load's unit tests
// rule out at build time.
func Default() *Registry {
	defaultOnce.Do(func() {
		defaultReg, defaultErr = Load()
	})
	if defaultErr != nil {
		panic(fmt.Sprintf("registry: embedded curated registry is invalid: %v", defaultErr))
	}
	return defaultReg
}

// Load parses and validates the embedded curated registry.
func Load() (*Registry, error) {
	r := &Registry{
		Attributes:           map[string]Attribute{},
		Operations:           map[string]*Operation{},
		Metrics:              map[string]*Metric{},
		Events:               map[string]*Event{},
		DeprecatedAttributes: map[string]Deprecation{},
		DeprecatedEvents:     map[string]Deprecation{},
		opByName:             map[string]*Operation{},
	}

	var meta struct {
		Semconv Meta `yaml:"semconv"`
	}
	if err := unmarshalFile("curated/meta.yaml", &meta); err != nil {
		return nil, err
	}
	r.Meta = meta.Semconv

	var attrs struct {
		Attributes map[string]Attribute `yaml:"attributes"`
	}
	if err := unmarshalFile("curated/attributes.yaml", &attrs); err != nil {
		return nil, err
	}
	r.Attributes = attrs.Attributes

	var spans struct {
		Operations map[string]*Operation `yaml:"operations"`
	}
	if err := unmarshalFile("curated/spans.yaml", &spans); err != nil {
		return nil, err
	}
	r.Operations = spans.Operations

	var metrics struct {
		Metrics map[string]*Metric `yaml:"metrics"`
	}
	if err := unmarshalFile("curated/metrics.yaml", &metrics); err != nil {
		return nil, err
	}
	r.Metrics = metrics.Metrics

	var events struct {
		Events map[string]*Event `yaml:"events"`
	}
	if err := unmarshalFile("curated/events.yaml", &events); err != nil {
		return nil, err
	}
	r.Events = events.Events

	var deps struct {
		DeprecatedAttributes map[string]Deprecation `yaml:"deprecated_attributes"`
		DeprecatedEvents     map[string]Deprecation `yaml:"deprecated_events"`
	}
	if err := unmarshalFile("curated/deprecations.yaml", &deps); err != nil {
		return nil, err
	}
	r.DeprecatedAttributes = deps.DeprecatedAttributes
	r.DeprecatedEvents = deps.DeprecatedEvents

	for key, op := range r.Operations {
		op.Key = key
	}
	for name, m := range r.Metrics {
		m.Name = name
	}
	for name, e := range r.Events {
		e.Name = name
	}

	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func unmarshalFile(path string, into any) error {
	raw, err := curatedFS.ReadFile(path)
	if err != nil {
		return fmt.Errorf("registry: reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("registry: parsing %s: %w", path, err)
	}
	return nil
}

func (r *Registry) validate() error {
	if r.Meta.SHA == "" || r.Meta.DocsBase == "" {
		return fmt.Errorf("meta.yaml: semconv pin (sha, docs_base) must be set")
	}
	if len(r.Attributes) == 0 {
		return fmt.Errorf("attributes.yaml: no attributes loaded")
	}

	// Operations: build the name index and check every referenced attribute
	// exists, kinds are legal, and name-format placeholders resolve.
	for _, key := range sortedKeys(r.Operations) {
		op := r.Operations[key]
		if len(op.OperationNames) == 0 {
			return fmt.Errorf("operation %s: operation_names must not be empty", key)
		}
		for _, name := range op.OperationNames {
			if prev, dup := r.opByName[name]; dup {
				return fmt.Errorf("operation name %q claimed by both %s and %s", name, prev.Key, key)
			}
			r.opByName[name] = op
		}
		if len(op.Kinds.Allowed) == 0 {
			return fmt.Errorf("operation %s: kinds.allowed must not be empty", key)
		}
		for _, k := range op.Kinds.Allowed {
			if !spanKindNames[k] {
				return fmt.Errorf("operation %s: unknown span kind %q", key, k)
			}
		}
		if err := r.checkLevels(fmt.Sprintf("operation %s", key), op.Attributes.Levels); err != nil {
			return err
		}
		for kind, keys := range op.Attributes.RequiredWhenKind {
			if err := r.checkAttrKeys(fmt.Sprintf("operation %s required_when_kind[%s]", key, kind), keys); err != nil {
				return err
			}
		}
		for kind, m := range op.Attributes.ConditionallyRequiredWhenKind {
			if err := r.checkAttrKeys(fmt.Sprintf("operation %s conditionally_required_when_kind[%s]", key, kind), mapKeys(m)); err != nil {
				return err
			}
		}
		for kind, keys := range op.Attributes.RecommendedWhenKind {
			if err := r.checkAttrKeys(fmt.Sprintf("operation %s recommended_when_kind[%s]", key, kind), keys); err != nil {
				return err
			}
		}
		for _, ph := range placeholderRE.FindAllStringSubmatch(op.NameFormat, -1) {
			if _, ok := r.Attributes[ph[1]]; !ok {
				return fmt.Errorf("operation %s: name_format references unknown attribute %q", key, ph[1])
			}
		}
	}

	for _, name := range sortedKeys(r.Metrics) {
		m := r.Metrics[name]
		if m.Instrument == "" || m.Unit == "" || m.ValueType == "" {
			return fmt.Errorf("metric %s: instrument, unit, and value_type must be set", name)
		}
		if err := r.checkLevels(fmt.Sprintf("metric %s", name), m.Attributes); err != nil {
			return err
		}
		if !sort.Float64sAreSorted(m.AdvisoryBuckets) {
			return fmt.Errorf("metric %s: advisory_buckets must be ascending", name)
		}
	}

	for _, name := range sortedKeys(r.Events) {
		if err := r.checkLevels(fmt.Sprintf("event %s", name), r.Events[name].Attributes); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) checkLevels(where string, l Levels) error {
	if err := r.checkAttrKeys(where+" required", l.Required); err != nil {
		return err
	}
	if err := r.checkAttrKeys(where+" conditionally_required", mapKeys(l.ConditionallyRequired)); err != nil {
		return err
	}
	if err := r.checkAttrKeys(where+" recommended", l.Recommended); err != nil {
		return err
	}
	return r.checkAttrKeys(where+" opt_in", l.OptIn)
}

func (r *Registry) checkAttrKeys(where string, keys []string) error {
	for _, k := range keys {
		if _, ok := r.Attributes[k]; !ok {
			return fmt.Errorf("%s: unknown attribute %q", where, k)
		}
	}
	return nil
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortedKeys returns the map's keys in ascending order so validation walks
// deterministically and error messages are stable run to run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
