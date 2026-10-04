package rules

import (
	"fmt"
	"strings"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanOperationName implements GENAI-SPAN-002: gen_ai.operation.name should
// use a predefined value unless a system-specific name is documented.
type spanOperationName struct {
	engine.Base
	reg *registry.Registry
}

func newSpanOperationName(reg *registry.Registry) engine.Rule {
	return &spanOperationName{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-002",
			Sev:      engine.SeverityWarning,
			RuleNote: "gen_ai.operation.name should be one of the predefined operation values.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanOperationName) CheckSpan(c *engine.SpanContext) []engine.Finding {
	name := c.OperationName()
	if name == "" {
		return nil // GENAI-SPAN-001 owns the missing case
	}
	if _, known := c.Registry.OperationForName(name); known {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "gen_ai.operation.name",
		fmt.Sprintf("operation name %q is not a predefined value", truncate(name, 64)),
		"use a predefined operation value, or document the system-specific name in your conventions")}
}

// spanName implements GENAI-SPAN-003: the span name should follow the
// operation's documented format, e.g. "chat {gen_ai.request.model}".
type spanName struct {
	engine.Base
	reg *registry.Registry
}

func newSpanName(reg *registry.Registry) engine.Rule {
	return &spanName{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-003",
			Sev:      engine.SeverityWarning,
			RuleNote: "Span names should follow the operation's format (e.g. \"chat {gen_ai.request.model}\").",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanName) CheckSpan(c *engine.SpanContext) []engine.Finding {
	op, ok := c.Operation()
	if !ok {
		return nil
	}
	want := expectedSpanName(op, c)
	if c.Span.Name() == want {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("span name %q should be %q", truncate(c.Span.Name(), 96), truncate(want, 96)),
		fmt.Sprintf("name %s spans %q", c.OperationName(), op.NameFormat))}
}

// spanKind implements GENAI-SPAN-004: span kind should match the
// operation's convention (e.g. CLIENT for inference, INTERNAL for tools).
type spanKind struct {
	engine.Base
	reg *registry.Registry
}

func newSpanKind(reg *registry.Registry) engine.Rule {
	return &spanKind{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-004",
			Sev:      engine.SeverityWarning,
			RuleNote: "Span kind should match the operation's convention (CLIENT for inference, INTERNAL for tool execution, ...).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanKind) CheckSpan(c *engine.SpanContext) []engine.Finding {
	op, ok := c.Operation()
	if !ok {
		return nil
	}
	kind := c.KindString()
	for _, allowed := range op.Kinds.Allowed {
		if kind == allowed {
			return nil
		}
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("span kind %q is not expected for %q spans (allowed: %s)", kind, c.OperationName(), strings.Join(op.Kinds.Allowed, ", ")),
		fmt.Sprintf("use span kind %s for %s spans", strings.ToUpper(op.Kinds.Preferred), c.OperationName()))}
}
