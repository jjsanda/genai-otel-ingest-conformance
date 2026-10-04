package rules

import (
	"fmt"
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// metricShape implements GENAI-METRIC-001: a known gen_ai.* metric must use
// the instrument type and unit the conventions define — a duration reported
// in "ms" under a unit of "s" corrupts every dashboard downstream.
type metricShape struct {
	engine.Base
	reg *registry.Registry
}

func newMetricShape(reg *registry.Registry) engine.Rule {
	return &metricShape{
		Base: engine.Base{
			RuleID:   "GENAI-METRIC-001",
			Sev:      engine.SeverityError,
			RuleNote: "GenAI metrics must use the defined instrument type and unit (e.g. histogram in \"s\").",
			RuleDoc:  reg.DocURL("gen-ai-metrics.md"),
		},
		reg: reg,
	}
}

func (r *metricShape) CheckMetric(c *engine.MetricContext) []engine.Finding {
	shape, ok := c.Shape()
	if !ok {
		return nil // GENAI-METRIC-003 owns unknown names
	}
	var out []engine.Finding
	if shape.Instrument == "histogram" {
		t := c.Metric.Type()
		// An exponential histogram is a legal SDK aggregation of a histogram
		// instrument, so both concrete types conform.
		if t != pmetric.MetricTypeHistogram && t != pmetric.MetricTypeExponentialHistogram {
			out = append(out, c.NewFinding(r, "",
				fmt.Sprintf("%s must be a histogram, got %s", c.Metric.Name(), t),
				"record this instrument as a histogram (explicit-bucket or exponential)"))
		}
	}
	if c.Metric.Unit() != shape.Unit {
		out = append(out, c.NewFinding(r, "",
			fmt.Sprintf("%s must use unit %q, got %q", c.Metric.Name(), shape.Unit, c.Metric.Unit()),
			fmt.Sprintf("set the instrument unit to %q (UCUM)", shape.Unit)))
	}
	return out
}

// metricRequiredAttrs implements GENAI-METRIC-002: required attributes must
// be present on every data point, with legal enum values and wire types.
// Findings aggregate per attribute key so a firehose of points yields one
// finding, not thousands.
type metricRequiredAttrs struct {
	engine.Base
	reg *registry.Registry
}

func newMetricRequiredAttrs(reg *registry.Registry) engine.Rule {
	return &metricRequiredAttrs{
		Base: engine.Base{
			RuleID:   "GENAI-METRIC-002",
			Sev:      engine.SeverityError,
			RuleNote: "Required metric attributes (e.g. gen_ai.token.type on token usage) must be present on every data point, with legal values.",
			RuleDoc:  reg.DocURL("gen-ai-metrics.md"),
		},
		reg: reg,
	}
}

func (r *metricRequiredAttrs) CheckMetric(c *engine.MetricContext) []engine.Finding {
	shape, ok := c.Shape()
	if !ok {
		return nil
	}
	points := dataPointAttrs(c.Metric)
	if len(points) == 0 {
		return nil
	}

	missing := map[string]int{}     // attr key -> points missing it
	badValue := map[string]string{} // attr key -> first offending description
	for _, attrs := range points {
		for _, key := range shape.Attributes.Required {
			v, present := attrs.Get(key)
			if !present {
				missing[key]++
				continue
			}
			r.checkValue(key, v, badValue)
		}
		// Non-required attrs still get enum/type checks when present.
		attrs.Range(func(key string, v pcommon.Value) bool {
			if registry.IsGenAI(key) && !slices.Contains(shape.Attributes.Required, key) {
				r.checkValue(key, v, badValue)
			}
			return true
		})
	}

	var out []engine.Finding
	for _, key := range sortedMapKeys(missing) {
		out = append(out, c.NewFinding(r, key,
			fmt.Sprintf("%s is required on %s but missing from %d of %d data points", key, c.Metric.Name(), missing[key], len(points)),
			fmt.Sprintf("record %s on every %s measurement", key, c.Metric.Name())))
	}
	for _, key := range sortedMapKeys(badValue) {
		out = append(out, c.NewFinding(r, key, badValue[key],
			"use the defined values and wire types for metric attributes"))
	}
	return out
}

func (r *metricRequiredAttrs) checkValue(key string, v pcommon.Value, bad map[string]string) {
	if _, seen := bad[key]; seen {
		return
	}
	attr, _, known := lookupAttr(r.reg, key)
	if !known {
		return
	}
	if got := typeMismatch(attr, v); got != "" {
		bad[key] = fmt.Sprintf("%s must be %s, got %s", key, wantTypeName(attr.Type), got)
		return
	}
	if len(attr.Enum) > 0 && !attr.EnumOpen && v.Type() == pcommon.ValueTypeStr && !slices.Contains(attr.Enum, v.Str()) {
		bad[key] = fmt.Sprintf("%s value %q is not one of the defined values (%s)", key, truncate(v.Str(), 64), joinSorted(attr.Enum))
	}
}

// metricUnknownName implements GENAI-METRIC-003.
type metricUnknownName struct {
	engine.Base
	reg *registry.Registry
}

func newMetricUnknownName(reg *registry.Registry) engine.Rule {
	return &metricUnknownName{
		Base: engine.Base{
			RuleID:   "GENAI-METRIC-003",
			Sev:      engine.SeverityWarning,
			RuleNote: "Unknown gen_ai.* metric name: likely a typo, or a newer conventions snapshot than the pin.",
			RuleDoc:  reg.DocURL("gen-ai-metrics.md"),
		},
		reg: reg,
	}
}

func (r *metricUnknownName) CheckMetric(c *engine.MetricContext) []engine.Finding {
	if _, known := c.Shape(); known {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("%s is not defined by the pinned GenAI conventions", c.Metric.Name()),
		"check for a typo; if the metric is from a newer conventions version, upgrade the suite's semconv pin")}
}

// metricAdvisoryBuckets implements GENAI-METRIC-004: the conventions advise
// explicit bucket boundaries per instrument; deviating is legal but makes
// histograms incomparable across services. INFO only.
type metricAdvisoryBuckets struct {
	engine.Base
	reg *registry.Registry
}

func newMetricAdvisoryBuckets(reg *registry.Registry) engine.Rule {
	return &metricAdvisoryBuckets{
		Base: engine.Base{
			RuleID:   "GENAI-METRIC-004",
			Sev:      engine.SeverityInfo,
			RuleNote: "Histogram bucket boundaries differ from the conventions' advice (legal, but hurts cross-service comparability).",
			RuleDoc:  reg.DocURL("gen-ai-metrics.md"),
		},
		reg: reg,
	}
}

func (r *metricAdvisoryBuckets) CheckMetric(c *engine.MetricContext) []engine.Finding {
	shape, ok := c.Shape()
	if !ok || len(shape.AdvisoryBuckets) == 0 || c.Metric.Type() != pmetric.MetricTypeHistogram {
		return nil // exponential histograms have no explicit bounds to compare
	}
	dps := c.Metric.Histogram().DataPoints()
	if dps.Len() == 0 {
		return nil
	}
	bounds := dps.At(0).ExplicitBounds()
	got := make([]float64, bounds.Len())
	for i := 0; i < bounds.Len(); i++ {
		got[i] = bounds.At(i)
	}
	if slices.Equal(got, shape.AdvisoryBuckets) {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "",
		fmt.Sprintf("%s uses custom bucket boundaries (%d bounds); the conventions advise %v", c.Metric.Name(), len(got), shape.AdvisoryBuckets),
		"configure a View with the advised ExplicitBucketBoundaries so histograms are comparable across services")}
}

// metricSecondsSanity implements GENAI-METRIC-005: a seconds-unit histogram
// whose values exceed an hour almost always means milliseconds were recorded
// as seconds — the unit label lies.
type metricSecondsSanity struct {
	engine.Base
	reg *registry.Registry
}

func newMetricSecondsSanity(reg *registry.Registry) engine.Rule {
	return &metricSecondsSanity{
		Base: engine.Base{
			RuleID:   "GENAI-METRIC-005",
			Sev:      engine.SeverityWarning,
			RuleNote: "Seconds-unit duration values exceed one hour — milliseconds recorded as seconds?",
			RuleDoc:  reg.DocURL("gen-ai-metrics.md"),
		},
		reg: reg,
	}
}

const suspiciousSeconds = 3600.0

func (r *metricSecondsSanity) CheckMetric(c *engine.MetricContext) []engine.Finding {
	shape, ok := c.Shape()
	if !ok || shape.Unit != "s" || c.Metric.Type() != pmetric.MetricTypeHistogram {
		return nil
	}
	dps := c.Metric.Histogram().DataPoints()
	for i := 0; i < dps.Len(); i++ {
		dp := dps.At(i)
		suspicious := dp.HasMax() && dp.Max() > suspiciousSeconds
		if !suspicious && dp.Count() > 0 && dp.HasSum() {
			suspicious = dp.Sum()/float64(dp.Count()) > suspiciousSeconds
		}
		if suspicious {
			return []engine.Finding{c.NewFinding(r, "",
				fmt.Sprintf("%s reports durations over an hour; the values are likely milliseconds mislabeled as seconds", c.Metric.Name()),
				"record durations in seconds (divide by 1000 if the source measures milliseconds)")}
		}
	}
	return nil
}

// dataPointAttrs flattens a metric's data-point attribute maps regardless of
// concrete type.
func dataPointAttrs(m pmetric.Metric) []pcommon.Map {
	var out []pcommon.Map
	switch m.Type() {
	case pmetric.MetricTypeHistogram:
		dps := m.Histogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			out = append(out, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeExponentialHistogram:
		dps := m.ExponentialHistogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			out = append(out, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeSum:
		dps := m.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			out = append(out, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeGauge:
		dps := m.Gauge().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			out = append(out, dps.At(i).Attributes())
		}
	case pmetric.MetricTypeSummary:
		dps := m.Summary().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			out = append(out, dps.At(i).Attributes())
		}
	}
	return out
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
