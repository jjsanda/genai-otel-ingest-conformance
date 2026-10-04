package rules

import (
	"fmt"
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// spanAttrTypes implements GENAI-SPAN-005: attribute values must use the
// wire type the conventions define (token counts are ints, finish reasons
// are string arrays, ...).
type spanAttrTypes struct {
	engine.Base
	reg *registry.Registry
}

func newSpanAttrTypes(reg *registry.Registry) engine.Rule {
	return &spanAttrTypes{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-005",
			Sev:      engine.SeverityError,
			RuleNote: "Attribute values must use the wire type the conventions define.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanAttrTypes) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, v pcommon.Value) bool {
		if !registry.IsGenAI(key) {
			return true
		}
		attr, _, known := lookupAttr(r.reg, key)
		if !known {
			return true // GENAI-SPAN-015 owns unknown attributes
		}
		if got := typeMismatch(attr, v); got != "" {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s must be %s, got %s", key, wantTypeName(attr.Type), got),
				fmt.Sprintf("emit %s as %s", key, wantTypeName(attr.Type))))
		}
		return true
	})
	return out
}

// spanAttrBounds implements GENAI-SPAN-006: numeric values must be within
// legal bounds (token counts and similar quantities are never negative).
type spanAttrBounds struct {
	engine.Base
	reg *registry.Registry
}

func newSpanAttrBounds(reg *registry.Registry) engine.Rule {
	return &spanAttrBounds{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-006",
			Sev:      engine.SeverityError,
			RuleNote: "Numeric attribute values must be within legal bounds (token counts are never negative).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanAttrBounds) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, v pcommon.Value) bool {
		if !registry.IsGenAI(key) {
			return true
		}
		attr, _, known := lookupAttr(r.reg, key)
		if !known || attr.Min == nil {
			return true
		}
		if num, ok := numericValue(v); ok && num < *attr.Min {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s is %v, below the minimum legal value %v", key, num, *attr.Min),
				fmt.Sprintf("report a non-negative value for %s (omit the attribute when the quantity is unknown)", key)))
		}
		return true
	})
	return out
}

// spanClosedEnums implements GENAI-SPAN-013: closed-enum attributes must use
// a defined value.
type spanClosedEnums struct {
	engine.Base
	reg *registry.Registry
}

func newSpanClosedEnums(reg *registry.Registry) engine.Rule {
	return &spanClosedEnums{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-013",
			Sev:      engine.SeverityError,
			RuleNote: "Closed-enum attributes (gen_ai.output.type, gen_ai.token.type) must use a defined value.",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanClosedEnums) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, v pcommon.Value) bool {
		if !registry.IsGenAI(key) || v.Type() != pcommon.ValueTypeStr {
			return true
		}
		attr, _, known := lookupAttr(r.reg, key)
		if !known || len(attr.Enum) == 0 || attr.EnumOpen {
			return true
		}
		if !slices.Contains(attr.Enum, v.Str()) {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s value %q is not one of the defined values (%s)", key, truncate(v.Str(), 64), joinSorted(attr.Enum)),
				fmt.Sprintf("use one of the defined %s values", key)))
		}
		return true
	})
	return out
}

// spanOpenEnums implements GENAI-SPAN-014: open-enum attributes carrying a
// custom value are legal, but well-known values integrate better — surfaced
// as a hint only.
type spanOpenEnums struct {
	engine.Base
	reg *registry.Registry
}

func newSpanOpenEnums(reg *registry.Registry) engine.Rule {
	return &spanOpenEnums{
		Base: engine.Base{
			RuleID:   "GENAI-SPAN-014",
			Sev:      engine.SeverityInfo,
			RuleNote: "Open-enum attribute uses a custom value (permitted; well-known values integrate better).",
			RuleDoc:  reg.DocURL("gen-ai-spans.md"),
		},
		reg: reg,
	}
}

func (r *spanOpenEnums) CheckSpan(c *engine.SpanContext) []engine.Finding {
	var out []engine.Finding
	c.Span.Attributes().Range(func(key string, v pcommon.Value) bool {
		if !registry.IsGenAI(key) || v.Type() != pcommon.ValueTypeStr {
			return true
		}
		if key == "gen_ai.operation.name" {
			return true // GENAI-SPAN-002 owns operation names
		}
		attr, _, known := lookupAttr(r.reg, key)
		if !known || len(attr.Enum) == 0 || !attr.EnumOpen {
			return true
		}
		if !slices.Contains(attr.Enum, v.Str()) {
			out = append(out, c.NewFinding(r, key,
				fmt.Sprintf("%s value %q is not a well-known value (custom values are permitted)", key, truncate(v.Str(), 64)),
				"custom values are fine for in-house systems; use the documented value when one exists for your provider"))
		}
		return true
	})
	return out
}
