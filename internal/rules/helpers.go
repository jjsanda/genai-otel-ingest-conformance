// Package rules implements the GenAI conformance rule catalog on top of the
// engine and the curated registry. Rule IDs are stable public API: they
// appear in reports, goldens, and CI assertions.
package rules

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// lookupAttr resolves an attribute key against the registry, handling
// template attributes (gen_ai.prompt.variable.<key>).
func lookupAttr(reg *registry.Registry, key string) (registry.Attribute, string, bool) {
	if a, ok := reg.Attributes[key]; ok {
		return a, key, true
	}
	for canonical, a := range reg.Attributes {
		if a.Type == registry.TypeTemplateString && strings.HasPrefix(key, canonical+".") {
			return a, canonical, true
		}
	}
	return registry.Attribute{}, "", false
}

// typeMismatch checks a value against the registry type. Int values are
// accepted where a double is expected (numerically lossless and common).
// It returns "" when the value conforms, else a human description of the
// actual type.
func typeMismatch(attr registry.Attribute, v pcommon.Value) string {
	got := v.Type()
	switch attr.Type {
	case registry.TypeString, registry.TypeTemplateString:
		if got != pcommon.ValueTypeStr {
			return valueTypeName(got)
		}
	case registry.TypeInt:
		if got != pcommon.ValueTypeInt {
			return valueTypeName(got)
		}
	case registry.TypeDouble:
		if got != pcommon.ValueTypeDouble && got != pcommon.ValueTypeInt {
			return valueTypeName(got)
		}
	case registry.TypeBool:
		if got != pcommon.ValueTypeBool {
			return valueTypeName(got)
		}
	case registry.TypeStringArray:
		if got != pcommon.ValueTypeSlice {
			return valueTypeName(got)
		}
		sl := v.Slice()
		for i := 0; i < sl.Len(); i++ {
			if sl.At(i).Type() != pcommon.ValueTypeStr {
				return fmt.Sprintf("array with %s element", valueTypeName(sl.At(i).Type()))
			}
		}
	case registry.TypeAny:
		// No wire-type constraint.
	}
	return ""
}

// wantTypeName renders the registry type for messages.
func wantTypeName(t registry.AttrType) string {
	switch t {
	case registry.TypeString:
		return "string"
	case registry.TypeInt:
		return "int"
	case registry.TypeDouble:
		return "double"
	case registry.TypeBool:
		return "boolean"
	case registry.TypeStringArray:
		return "string array"
	case registry.TypeTemplateString:
		return "string (template attribute)"
	default:
		return string(t)
	}
}

func valueTypeName(t pcommon.ValueType) string {
	switch t {
	case pcommon.ValueTypeStr:
		return "string"
	case pcommon.ValueTypeInt:
		return "int"
	case pcommon.ValueTypeDouble:
		return "double"
	case pcommon.ValueTypeBool:
		return "boolean"
	case pcommon.ValueTypeSlice:
		return "array"
	case pcommon.ValueTypeMap:
		return "map"
	case pcommon.ValueTypeBytes:
		return "bytes"
	default:
		return "empty"
	}
}

// numericValue extracts an int or double attribute value as float64.
func numericValue(v pcommon.Value) (float64, bool) {
	switch v.Type() {
	case pcommon.ValueTypeInt:
		return float64(v.Int()), true
	case pcommon.ValueTypeDouble:
		return v.Double(), true
	default:
		return 0, false
	}
}

// expectedSpanName resolves an operation's name_format for a concrete span:
// the leading {gen_ai.operation.name} placeholder becomes the span's actual
// operation name, and a trailing placeholder becomes " <value>" when the
// attribute is present or disappears when it is not.
//
// The template is scanned exactly once and resolved values are appended to a
// builder rather than spliced back into the scanned string — otherwise an
// attribute whose value itself contains "{...}" (e.g. a span reporting
// gen_ai.request.model = "{gen_ai.request.model}") would be rescanned
// forever, wedging the ingest goroutine on a single crafted span.
func expectedSpanName(op *registry.Operation, c *engine.SpanContext) string {
	var b strings.Builder
	tmpl := op.NameFormat
	i := 0
	for i < len(tmpl) {
		open := strings.IndexByte(tmpl[i:], '{')
		if open < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		open += i
		closeIdx := strings.IndexByte(tmpl[open:], '}')
		if closeIdx < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		closeIdx += open
		b.WriteString(tmpl[i:open]) // literal text before the placeholder

		key := tmpl[open+1 : closeIdx]
		var val string
		if key == "gen_ai.operation.name" {
			val = c.OperationName()
		} else if v, ok := c.Attr(key); ok && v.Type() == pcommon.ValueTypeStr {
			val = v.Str()
		}
		if val == "" {
			// Attribute absent/empty: the bare prefix (name minus the
			// trailing " {placeholder}") is the expected name.
			return strings.TrimRight(b.String(), " ")
		}
		b.WriteString(val)
		i = closeIdx + 1
	}
	return b.String()
}

// truncate keeps messages bounded when echoing non-content values.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// joinSorted renders an attribute list for a single grouped message.
func joinSorted(keys []string) string {
	return strings.Join(keys, ", ")
}
