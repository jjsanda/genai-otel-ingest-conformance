package rules

import (
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// resServiceName implements GENAI-RES-001: telemetry without a real
// service.name cannot be attributed, aggregated, or alerted on.
type resServiceName struct {
	engine.Base
	reg *registry.Registry
}

func newResServiceName(reg *registry.Registry) engine.Rule {
	return &resServiceName{
		Base: engine.Base{
			RuleID:   "GENAI-RES-001",
			Sev:      engine.SeverityError,
			RuleNote: "service.name must be set to a real value (not unknown_service).",
			RuleDoc:  "https://opentelemetry.io/docs/specs/semconv/resource/#service",
		},
		reg: reg,
	}
}

func (r *resServiceName) CheckResource(c *engine.ResourceContext) []engine.Finding {
	v, ok := c.Attr("service.name")
	if ok && v.Type() == pcommon.ValueTypeStr && v.Str() != "" && !strings.HasPrefix(v.Str(), "unknown_service") {
		return nil
	}
	msg := "resource has no service.name; GenAI telemetry cannot be attributed to a service"
	if ok {
		msg = "service.name is the SDK fallback (unknown_service*); set a real service name"
	}
	return []engine.Finding{c.NewFinding(r, "service.name", msg,
		"set OTEL_SERVICE_NAME or the service.name resource attribute in the SDK configuration")}
}

// resServiceVersion implements GENAI-RES-002.
type resServiceVersion struct {
	engine.Base
	reg *registry.Registry
}

func newResServiceVersion(reg *registry.Registry) engine.Rule {
	return &resServiceVersion{
		Base: engine.Base{
			RuleID:   "GENAI-RES-002",
			Sev:      engine.SeverityWarning,
			RuleNote: "service.version should be set; without it regressions cannot be tied to deployments.",
			RuleDoc:  "https://opentelemetry.io/docs/specs/semconv/resource/#service",
		},
		reg: reg,
	}
}

func (r *resServiceVersion) CheckResource(c *engine.ResourceContext) []engine.Finding {
	if _, ok := c.Attr("service.version"); ok {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, "service.version",
		"resource has no service.version",
		"set service.version so quality or cost regressions can be tied to a specific deployment")}
}

// resTelemetrySDK implements GENAI-RES-003: SDKs populate telemetry.sdk.*
// automatically, so their absence usually means a hand-rolled exporter.
type resTelemetrySDK struct {
	engine.Base
	reg *registry.Registry
}

func newResTelemetrySDK(reg *registry.Registry) engine.Rule {
	return &resTelemetrySDK{
		Base: engine.Base{
			RuleID:   "GENAI-RES-003",
			Sev:      engine.SeverityInfo,
			RuleNote: "telemetry.sdk.* attributes are absent; official SDKs set them automatically.",
			RuleDoc:  "https://opentelemetry.io/docs/specs/semconv/resource/#telemetry-sdk",
		},
		reg: reg,
	}
}

func (r *resTelemetrySDK) CheckResource(c *engine.ResourceContext) []engine.Finding {
	var missing []string
	for _, key := range []string{"telemetry.sdk.name", "telemetry.sdk.language", "telemetry.sdk.version"} {
		if _, ok := c.Attr(key); !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []engine.Finding{c.NewFinding(r, joinSorted(missing),
		"resource is missing "+joinSorted(missing)+"; official SDKs populate these automatically",
		"if you export OTLP without an SDK, set the telemetry.sdk.* resource attributes yourself")}
}
