package rules

import (
	"fmt"
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// Event rules check GenAI log records (log records carrying an event_name).
// The headline case is evaluation telemetry: gen_ai.evaluation.result events
// are how pass rates, groundedness, and tool precision reach dashboards.

// eventShapeRule implements GENAI-EVENT-001: known events must carry their
// required attributes with legal types and enum values.
type eventShapeRule struct {
	engine.Base
	reg *registry.Registry
}

func newEventShape(reg *registry.Registry) engine.Rule {
	return &eventShapeRule{
		Base: engine.Base{
			RuleID:   "GENAI-EVENT-001",
			Sev:      engine.SeverityError,
			RuleNote: "GenAI events must carry their required attributes (e.g. gen_ai.evaluation.name on evaluation results).",
			RuleDoc:  reg.DocURL("gen-ai-events.md"),
		},
		reg: reg,
	}
}

func (r *eventShapeRule) CheckLog(c *engine.LogContext) []engine.Finding {
	shape, ok := c.EventShape()
	if !ok {
		return nil // GENAI-EVENT-003 owns unknown names
	}
	var out []engine.Finding
	for _, key := range shape.Attributes.Required {
		if _, present := c.Attr(key); !present {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s is required on %s events but is missing", key, shape.Name),
				fmt.Sprintf("set %s on every %s event", key, shape.Name)))
		}
	}
	// The exceptions doc defines an at-least-one-of pair (each attribute is
	// required exactly when the other is absent) — a shape the flat Levels
	// model cannot express, so it is enforced here directly.
	if shape.Name == "gen_ai.client.operation.exception" {
		_, hasType := c.Attr("exception.type")
		_, hasMessage := c.Attr("exception.message")
		if !hasType && !hasMessage {
			out = append(out, c.NewFinding(r, "exception.type",
				"exception event carries neither exception.type nor exception.message; at least one is required",
				"set exception.type (canonical class) and/or exception.message on every operation exception"))
		}
	}
	c.Record.Attributes().Range(func(key string, v pcommon.Value) bool {
		attr, _, known := lookupAttr(r.reg, key)
		if !known {
			return true
		}
		if got := typeMismatch(attr, v); got != "" {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s must be %s, got %s", key, wantTypeName(attr.Type), got),
				fmt.Sprintf("emit %s as %s", key, wantTypeName(attr.Type))))
			return true
		}
		if len(attr.Enum) > 0 && !attr.EnumOpen && v.Type() == pcommon.ValueTypeStr && !slices.Contains(attr.Enum, v.Str()) {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s value %q is not one of the defined values (%s)", key, truncate(v.Str(), 64), joinSorted(attr.Enum)),
				fmt.Sprintf("use one of the defined %s values", key)))
		}
		return true
	})
	return out
}

// eventEvaluationAssociation implements GENAI-EVENT-002: an evaluation
// result that cannot be joined to the operation it evaluated is a number
// without a story — the conventions require a span link or a response id.
type eventEvaluationAssociation struct {
	engine.Base
	reg *registry.Registry
}

func newEventEvaluationAssociation(reg *registry.Registry) engine.Rule {
	return &eventEvaluationAssociation{
		Base: engine.Base{
			RuleID:   "GENAI-EVENT-002",
			Sev:      engine.SeverityWarning,
			RuleNote: "Evaluation events should be parented to the evaluated span (trace/span id) or carry gen_ai.response.id.",
			RuleDoc:  reg.DocURL("gen-ai-events.md"),
		},
		reg: reg,
	}
}

func (r *eventEvaluationAssociation) CheckLog(c *engine.LogContext) []engine.Finding {
	if c.Record.EventName() != "gen_ai.evaluation.result" {
		return nil
	}
	hasSpan := !c.Record.TraceID().IsEmpty() && !c.Record.SpanID().IsEmpty()
	_, hasResponseID := c.Attr("gen_ai.response.id")
	if hasSpan || hasResponseID {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "gen_ai.response.id",
		"evaluation result carries neither a trace/span association nor gen_ai.response.id — it cannot be joined to the operation it evaluated",
		"emit the event with the evaluated span's context, or set gen_ai.response.id when the span is unavailable")}
}

// eventUnknownName implements GENAI-EVENT-003.
type eventUnknownName struct {
	engine.Base
	reg *registry.Registry
}

func newEventUnknownName(reg *registry.Registry) engine.Rule {
	return &eventUnknownName{
		Base: engine.Base{
			RuleID:   "GENAI-EVENT-003",
			Sev:      engine.SeverityWarning,
			RuleNote: "Unknown gen_ai.* event name: a typo, a legacy shape, or a newer conventions snapshot than the pin.",
			RuleDoc:  reg.DocURL("gen-ai-events.md"),
		},
		reg: reg,
	}
}

func (r *eventUnknownName) CheckLog(c *engine.LogContext) []engine.Finding {
	name := c.Record.EventName()
	if !registry.IsGenAI(name) {
		return nil // GenAI-relevant by attributes only; no event-name contract to check
	}
	if _, known := c.EventShape(); known {
		return nil
	}
	if dep, isLegacy := r.reg.DeprecatedEvents[name]; isLegacy {
		return []engine.Finding{c.NewFinding(r, "",
			fmt.Sprintf("event %q is a legacy content-event shape; the current conventions record content in the opt-in %s attribute", name, dep.Replacement),
			fmt.Sprintf("migrate to the %s span attribute (opt-in)", dep.Replacement))}
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("event %q is not defined by the pinned GenAI conventions", name),
		"check for a typo; if the event is from a newer conventions version, upgrade the suite's semconv pin")}
}

// eventExceptionSeverity implements GENAI-EVENT-004: the conventions say
// GenAI operation exceptions SHOULD be recorded at WARN (severity number 13)
// so they alert without masquerading as process-fatal errors.
type eventExceptionSeverity struct {
	engine.Base
	reg *registry.Registry
}

func newEventExceptionSeverity(reg *registry.Registry) engine.Rule {
	return &eventExceptionSeverity{
		Base: engine.Base{
			RuleID:   "GENAI-EVENT-004",
			Sev:      engine.SeverityWarning,
			RuleNote: "gen_ai.client.operation.exception events should be recorded at severity WARN (13).",
			RuleDoc:  reg.DocURL("gen-ai-exceptions.md"),
		},
		reg: reg,
	}
}

func (r *eventExceptionSeverity) CheckLog(c *engine.LogContext) []engine.Finding {
	shape, ok := c.EventShape()
	if !ok || shape.SeverityNumber == 0 {
		return nil
	}
	if int(c.Record.SeverityNumber()) == shape.SeverityNumber {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("%s recorded at severity number %d; the conventions say WARN (%d)", shape.Name, c.Record.SeverityNumber(), shape.SeverityNumber),
		fmt.Sprintf("set the log record severity number to %d (WARN)", shape.SeverityNumber))}
}
