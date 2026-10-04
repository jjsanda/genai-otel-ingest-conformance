package registry

import (
	"slices"
	"testing"
)

func mustLoad(t *testing.T) *Registry {
	t.Helper()
	r, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	return r
}

func TestLoadValidates(t *testing.T) {
	r := mustLoad(t)
	if r.Meta.SHA != "b028dceecdad117461a785c3af35315e7184e813" {
		t.Errorf("unexpected pinned SHA %q", r.Meta.SHA)
	}
	if got := len(r.opByName); got != 17 {
		t.Errorf("operation name index has %d entries, want 17", got)
	}
}

func TestChatResolvesToInference(t *testing.T) {
	r := mustLoad(t)
	op, ok := r.OperationForName("chat")
	if !ok {
		t.Fatal("chat not resolved")
	}
	if op.Key != "inference" {
		t.Errorf("chat resolved to %q, want inference", op.Key)
	}
	if !slices.Contains(op.Attributes.Required, "gen_ai.provider.name") {
		t.Error("inference spans must require gen_ai.provider.name")
	}
	if op.NameFormat != "{gen_ai.operation.name} {gen_ai.request.model}" {
		t.Errorf("unexpected name format %q", op.NameFormat)
	}
}

func TestInvokeAgentProviderRequiredOnlyOnClientKind(t *testing.T) {
	r := mustLoad(t)
	op, ok := r.OperationForName("invoke_agent")
	if !ok {
		t.Fatal("invoke_agent not resolved")
	}
	if slices.Contains(op.Attributes.Required, "gen_ai.provider.name") {
		t.Error("gen_ai.provider.name must not be unconditionally required on invoke_agent")
	}
	client := op.EffectiveLevels("client")
	if !slices.Contains(client.Required, "gen_ai.provider.name") {
		t.Error("client-kind invoke_agent must require gen_ai.provider.name")
	}
	internal := op.EffectiveLevels("internal")
	if slices.Contains(internal.Required, "gen_ai.provider.name") {
		t.Error("internal-kind invoke_agent must not require gen_ai.provider.name")
	}
}

func TestExecuteToolRequiresToolName(t *testing.T) {
	r := mustLoad(t)
	op, ok := r.OperationForName("execute_tool")
	if !ok {
		t.Fatal("execute_tool not resolved")
	}
	if !slices.Contains(op.Attributes.Required, "gen_ai.tool.name") {
		t.Error("execute_tool spans must require gen_ai.tool.name")
	}
	if !slices.Equal(op.Kinds.Allowed, []string{"internal"}) {
		t.Errorf("execute_tool allowed kinds = %v, want [internal]", op.Kinds.Allowed)
	}
}

func TestTokenUsageMetricShape(t *testing.T) {
	r := mustLoad(t)
	m, ok := r.Metrics["gen_ai.client.token.usage"]
	if !ok {
		t.Fatal("gen_ai.client.token.usage missing")
	}
	if m.Unit != "{token}" || m.Instrument != "histogram" || m.ValueType != "int" {
		t.Errorf("unexpected shape: unit=%q instrument=%q value_type=%q", m.Unit, m.Instrument, m.ValueType)
	}
	if !slices.Contains(m.Attributes.Required, "gen_ai.token.type") {
		t.Error("token.usage must require gen_ai.token.type")
	}
	if len(m.AdvisoryBuckets) == 0 {
		t.Error("token.usage should carry advisory buckets")
	}
}

func TestAgenticDurationMetricsHaveNoUnconditionalRequirements(t *testing.T) {
	r := mustLoad(t)
	for _, name := range []string{"gen_ai.workflow.duration", "gen_ai.invoke_agent.duration"} {
		m, ok := r.Metrics[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		if len(m.Attributes.Required) != 0 {
			t.Errorf("%s: required = %v, want none", name, m.Attributes.Required)
		}
	}
	if m := r.Metrics["gen_ai.execute_tool.duration"]; !slices.Contains(m.Attributes.Required, "gen_ai.tool.name") {
		t.Error("execute_tool.duration must require gen_ai.tool.name")
	}
}

func TestEvaluationEventShape(t *testing.T) {
	r := mustLoad(t)
	e, ok := r.Events["gen_ai.evaluation.result"]
	if !ok {
		t.Fatal("gen_ai.evaluation.result missing")
	}
	if !slices.Contains(e.Attributes.Required, "gen_ai.evaluation.name") {
		t.Error("evaluation.result must require gen_ai.evaluation.name")
	}
	if e.Association == "" {
		t.Error("evaluation.result should document its span/response association")
	}
}

func TestDeprecationsCoverKnownRenames(t *testing.T) {
	r := mustLoad(t)
	for legacy, want := range map[string]string{
		"gen_ai.system":                  "gen_ai.provider.name",
		"gen_ai.usage.prompt_tokens":     "gen_ai.usage.input_tokens",
		"gen_ai.usage.completion_tokens": "gen_ai.usage.output_tokens",
	} {
		dep, ok := r.DeprecatedAttributes[legacy]
		if !ok {
			t.Errorf("missing deprecation for %s", legacy)
			continue
		}
		if dep.Replacement != want {
			t.Errorf("%s replacement = %q, want %q", legacy, dep.Replacement, want)
		}
	}
	if _, ok := r.DeprecatedEvents["gen_ai.content.prompt"]; !ok {
		t.Error("legacy content events must be mapped")
	}
}

func TestDocURL(t *testing.T) {
	r := mustLoad(t)
	got := r.DocURL("gen-ai-spans.md")
	want := "https://github.com/open-telemetry/semantic-conventions-genai/blob/b028dceecdad117461a785c3af35315e7184e813/docs/gen-ai/gen-ai-spans.md"
	if got != want {
		t.Errorf("DocURL = %q, want %q", got, want)
	}
}

func TestDefaultIsCachedAndValid(t *testing.T) {
	first := Default()
	second := Default()
	if first == nil || first != second {
		t.Error("Default() must return one cached, non-nil instance")
	}
}
