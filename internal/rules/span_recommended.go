package rules

import (
	"fmt"
	"slices"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// keyRecommended is the subset of recommended attributes whose absence hurts
// the most in practice: without token usage there is no cost accounting, and
// without response metadata there is no model-drift visibility. Only these
// are reported, one grouped finding per span, to keep the signal useful
// instead of pedantic.
var keyRecommended = map[string][]string{
	"inference":  {"gen_ai.request.model", "gen_ai.response.model", "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.response.finish_reasons"},
	"embeddings": {"gen_ai.request.model", "gen_ai.usage.input_tokens"},
}

// spanRecommended implements GENAI-SPAN-010.
type spanRecommended struct {
	engine.Base
	reg *registry.Registry
}

func newSpanRecommended(reg *registry.Registry) engine.Rule {
	return &spanRecommended{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-010",
			Sev:      engine.SeverityWarning,
			RuleNote: "Key recommended attributes should be present: token usage and response metadata drive cost and drift dashboards.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanRecommended) CheckSpan(c *engine.SpanContext) []engine.Finding {
	op, ok := c.Operation()
	if !ok {
		return nil
	}
	watch, ok := keyRecommended[op.Key]
	if !ok {
		return nil
	}
	levels := op.EffectiveLevels(c.KindString())
	var missing []string
	for _, key := range watch {
		// Only nag about attributes the conventions actually list for this
		// operation and kind, and that the span does not carry.
		if !slices.Contains(levels.Recommended, key) && !hasCondition(levels, key) {
			continue
		}
		if !c.HasAttr(key) {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, joinSorted(missing),
		fmt.Sprintf("%s span is missing recommended attributes: %s", c.OperationName(), joinSorted(missing)),
		"populate token usage and response metadata; they power cost accounting, latency, and model-drift dashboards")}
}

func hasCondition(levels registry.Levels, key string) bool {
	_, ok := levels.ConditionallyRequired[key]
	return ok
}
