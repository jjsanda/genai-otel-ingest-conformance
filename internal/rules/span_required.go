package rules

import (
	"fmt"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanRequired implements GENAI-SPAN-001: every GenAI span must carry
// gen_ai.operation.name, and the operation's required attributes (resolved
// per span kind) must be present.
type spanRequired struct {
	engine.Base
	reg *registry.Registry
}

func newSpanRequired(reg *registry.Registry) engine.Rule {
	return &spanRequired{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-001",
			Sev:      engine.SeverityError,
			RuleNote: "GenAI spans must carry gen_ai.operation.name and the operation's required attributes (span-kind aware).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanRequired) CheckSpan(c *engine.SpanContext) []engine.Finding {
	if c.OperationName() == "" {
		return []engine.Finding{c.NewFinding(r, "gen_ai.operation.name",
			"span carries gen_ai.* attributes but no gen_ai.operation.name; every GenAI span must declare its operation",
			"set gen_ai.operation.name to one of the predefined operation values (chat, execute_tool, invoke_agent, retrieval, ...)")}
	}
	op, ok := c.Operation()
	if !ok {
		// Unknown operation names are GENAI-SPAN-002's concern; without a
		// matrix there is nothing further to require here.
		return nil
	}
	var out []engine.Finding
	levels := op.EffectiveLevels(c.KindString())
	for _, key := range levels.Required {
		if !c.HasAttr(key) {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s is required on %q spans but is missing", key, c.OperationName()),
				fmt.Sprintf("set %s on every %s span", key, c.OperationName())))
		}
	}
	return out
}
