package rules

import (
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanContentCapture implements GENAI-SPAN-011: content-capture attributes
// (gen_ai.input.messages, gen_ai.system_instructions, tool arguments, ...)
// are legal but opt-in and may carry prompts, PII, and secrets. Their
// presence is surfaced as a hint so operators consciously confirm the
// opt-in and their redaction story. The values themselves are never read
// or echoed by this suite.
type spanContentCapture struct {
	engine.Base
	reg *registry.Registry
}

func newSpanContentCapture(reg *registry.Registry) engine.Rule {
	return &spanContentCapture{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-011",
			Sev:      engine.SeverityInfo,
			RuleNote: "Content-capture attributes detected; they are opt-in and may carry prompts/PII — confirm the opt-in and redaction policy.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanContentCapture) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var captured []string
	c.Span.Attributes().Range(func(key string, _ pcommon.Value) bool {
		if attr, _, known := lookupAttr(r.reg, key); known && attr.Content {
			captured = append(captured, key)
		}
		return true
	})
	if len(captured) == 0 {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, joinSorted(captured),
		"span captures message/tool content ("+joinSorted(captured)+"); the conventions treat content capture as opt-in",
		"confirm content capture is explicitly enabled (e.g. OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT) and that a redaction policy covers prompts and PII")}
}
