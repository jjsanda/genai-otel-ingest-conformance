package rules

import (
	"fmt"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// Most conditionally-required attributes have conditions only the emitting
// application can decide ("if available", "when applicable"). The two rules
// here cover the conditions that ARE decidable from the telemetry itself.

// spanErrorType implements GENAI-SPAN-007: error.type is required when the
// span status is Error.
type spanErrorType struct {
	engine.Base
	reg *registry.Registry
}

func newSpanErrorType(reg *registry.Registry) engine.Rule {
	return &spanErrorType{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-007",
			Sev:      engine.SeverityError,
			RuleNote: "error.type is required when the span status is Error.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanErrorType) CheckSpan(c *engine.SpanContext) []engine.Finding {
	if c.Span.Status().Code() != ptrace.StatusCodeError {
		return nil
	}
	if c.HasAttr("error.type") {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "error.type",
		"span status is Error but error.type is missing",
		"set error.type to the provider's error code, the exception's canonical name, or another low-cardinality identifier")}
}

// spanServerPort implements GENAI-SPAN-008: server.port is required when
// server.address is set.
type spanServerPort struct {
	engine.Base
	reg *registry.Registry
}

func newSpanServerPort(reg *registry.Registry) engine.Rule {
	return &spanServerPort{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-008",
			Sev:      engine.SeverityError,
			RuleNote: "server.port is required when server.address is set.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanServerPort) CheckSpan(c *engine.SpanContext) []engine.Finding {
	op, ok := c.Operation()
	if !ok {
		return nil
	}
	// Only operations whose matrix actually lists the server.port condition
	// (per span kind): an INTERNAL execute_tool span carrying a
	// server.address has no port requirement in the pinned conventions,
	// and inventing one would be a false positive at ERROR severity.
	levels := op.EffectiveLevels(c.KindString())
	if _, listed := levels.ConditionallyRequired["server.port"]; !listed {
		return nil
	}
	if !c.HasAttr("server.address") || c.HasAttr("server.port") {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "server.port",
		fmt.Sprintf("server.address is set on this %s span but server.port is missing", c.OperationName()),
		"set server.port alongside server.address (e.g. 443)")}
}
