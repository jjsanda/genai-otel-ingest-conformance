package rules

import (
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// All returns the complete conformance rule catalog bound to a registry.
// Rule IDs are stable: renumbering or reusing an ID is a breaking change for
// every CI assertion built on top of this suite.
func All(reg *registry.Registry) []engine.Rule {
	return []engine.Rule{
		// Span rules.
		newSpanRequired(reg),            // GENAI-SPAN-001
		newSpanOperationName(reg),       // GENAI-SPAN-002
		newSpanName(reg),                // GENAI-SPAN-003
		newSpanKind(reg),                // GENAI-SPAN-004
		newSpanAttrTypes(reg),           // GENAI-SPAN-005
		newSpanAttrBounds(reg),          // GENAI-SPAN-006
		newSpanErrorType(reg),           // GENAI-SPAN-007
		newSpanServerPort(reg),          // GENAI-SPAN-008
		newSpanDeprecatedAttrs(reg),     // GENAI-SPAN-009
		newSpanRecommended(reg),         // GENAI-SPAN-010
		newSpanContentCapture(reg),      // GENAI-SPAN-011
		newSpanLegacyContentEvents(reg), // GENAI-SPAN-012
		newSpanClosedEnums(reg),         // GENAI-SPAN-013
		newSpanOpenEnums(reg),           // GENAI-SPAN-014
		newSpanUnknownAttrs(reg),        // GENAI-SPAN-015

		// Trace-topology rules (assembled traces).
		newTraceToolAncestry(reg),  // GENAI-TRACE-001
		newTraceOrphanParents(reg), // GENAI-TRACE-002
		newTraceHasRoot(reg),       // GENAI-TRACE-003
		newTraceChildTiming(reg),   // GENAI-TRACE-004
		newTraceLateArrival(reg),   // GENAI-TRACE-005

		// Metric rules.
		newMetricShape(reg),           // GENAI-METRIC-001
		newMetricRequiredAttrs(reg),   // GENAI-METRIC-002
		newMetricUnknownName(reg),     // GENAI-METRIC-003
		newMetricAdvisoryBuckets(reg), // GENAI-METRIC-004
		newMetricSecondsSanity(reg),   // GENAI-METRIC-005

		// Event (log record) rules.
		newEventShape(reg),                 // GENAI-EVENT-001
		newEventEvaluationAssociation(reg), // GENAI-EVENT-002
		newEventUnknownName(reg),           // GENAI-EVENT-003
		newEventExceptionSeverity(reg),     // GENAI-EVENT-004

		// Resource rules.
		newResServiceName(reg),    // GENAI-RES-001
		newResServiceVersion(reg), // GENAI-RES-002
		newResTelemetrySDK(reg),   // GENAI-RES-003
	}
}
