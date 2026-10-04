package rules

import (
	"fmt"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanDeprecatedAttrs implements GENAI-SPAN-009: deprecated attributes must
// be migrated to their replacements (gen_ai.system → gen_ai.provider.name,
// prompt/completion token names, ...). Consumers reading the current
// conventions cannot see data emitted under the old names.
type spanDeprecatedAttrs struct {
	engine.Base
	reg *registry.Registry
}

func newSpanDeprecatedAttrs(reg *registry.Registry) engine.Rule {
	return &spanDeprecatedAttrs{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-009",
			Sev:      engine.SeverityError,
			RuleNote: "Deprecated attributes must be migrated to their replacements (gen_ai.system → gen_ai.provider.name, ...).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanDeprecatedAttrs) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, _ pcommon.Value) bool {
		if dep, ok := r.reg.DeprecatedAttributes[key]; ok {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s is deprecated; the current conventions use %s", key, dep.Replacement),
				fmt.Sprintf("rename %s to %s (instrumentation libraries migrate via OTEL_SEMCONV_STABILITY_OPT_IN=gen_ai_latest_experimental)", key, dep.Replacement)))
		}
		return true
	})
	return out
}

// spanLegacyContentEvents implements GENAI-SPAN-012: legacy span events that
// carried prompt/completion content must be migrated to the opt-in content
// attributes.
type spanLegacyContentEvents struct {
	engine.Base
	reg *registry.Registry
}

func newSpanLegacyContentEvents(reg *registry.Registry) engine.Rule {
	return &spanLegacyContentEvents{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-012",
			Sev:      engine.SeverityError,
			RuleNote: "Legacy content span events (gen_ai.content.prompt, gen_ai.choice, ...) must be migrated to content attributes.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanLegacyContentEvents) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	for i := 0; i < c.Span.Events().Len(); i++ {
		name := c.Span.Events().At(i).Name()
		if dep, ok := r.reg.DeprecatedEvents[name]; ok {
			out = append(out, c.NewFinding(r, "",
				fmt.Sprintf("span event %q is a legacy content event; the current conventions record content in the opt-in %s attribute", name, dep.Replacement),
				fmt.Sprintf("drop the %s span event and record content via %s (opt-in)", name, dep.Replacement)))
		}
	}
	return out
}
