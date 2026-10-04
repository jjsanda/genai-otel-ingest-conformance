package rules

import (
	"fmt"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanUnknownAttrs implements GENAI-SPAN-015: gen_ai.* attributes the pinned
// conventions do not define are usually typos (gen_ai.usage.total_tokens) or
// telemetry from a newer conventions snapshot than this suite is pinned to.
type spanUnknownAttrs struct {
	engine.Base
	reg *registry.Registry
}

func newSpanUnknownAttrs(reg *registry.Registry) engine.Rule {
	return &spanUnknownAttrs{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-015",
			Sev:      engine.SeverityWarning,
			RuleNote: "Unknown gen_ai.* attribute: likely a typo, or telemetry from a newer conventions snapshot than the pin.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanUnknownAttrs) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, _ pcommon.Value) bool {
		if !registry.IsGenAI(key) {
			return true
		}
		if _, ok := r.reg.DeprecatedAttributes[key]; ok {
			return true // GENAI-SPAN-009 owns deprecated attributes
		}
		if _, _, known := lookupAttr(r.reg, key); !known {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s is not defined by the pinned GenAI conventions", key),
				"check for a typo; if the attribute is from a newer conventions version, upgrade the suite's semconv pin"))
		}
		return true
	})
	return out
}
