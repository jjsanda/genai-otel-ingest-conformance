package rules

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// evalOne runs a single rule against a single constructed span and returns
// its findings, exercising the real engine walk.
func evalOne(t *testing.T, factory func(*registry.Registry) engine.Rule, build func(span ptrace.Span)) []engine.Finding {
	t.Helper()
	reg := registry.Default()
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "unit-test")
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{0xaa, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}))
	span.SetSpanID(pcommon.SpanID([8]byte{0xbb, 1, 2, 3, 4, 5, 6, 7}))
	build(span)
	// Rules only see the span if the engine classifies it as GenAI, which
	// requires at least one gen_ai.* attribute — build() must ensure that.
	eng := engine.New(reg, factory(reg))
	return eng.EvaluateTraces(td).Findings
}

func TestRequiredProviderDependsOnInvokeAgentKind(t *testing.T) {
	internal := evalOne(t, newSpanRequired, func(s ptrace.Span) {
		s.SetName("invoke_agent helper")
		s.SetKind(ptrace.SpanKindInternal)
		s.Attributes().PutStr("gen_ai.operation.name", "invoke_agent")
		s.Attributes().PutStr("gen_ai.agent.name", "helper")
	})
	if len(internal) != 0 {
		t.Errorf("internal invoke_agent must not require provider.name, got %v", internal)
	}

	client := evalOne(t, newSpanRequired, func(s ptrace.Span) {
		s.SetName("invoke_agent helper")
		s.SetKind(ptrace.SpanKindClient)
		s.Attributes().PutStr("gen_ai.operation.name", "invoke_agent")
		s.Attributes().PutStr("gen_ai.agent.name", "helper")
	})
	if len(client) != 1 || client[0].Attribute != "gen_ai.provider.name" {
		t.Errorf("client invoke_agent must require provider.name, got %v", client)
	}
}

func TestMissingOperationNameIsRequired(t *testing.T) {
	got := evalOne(t, newSpanRequired, func(s ptrace.Span) {
		s.SetName("chat gpt-4")
		s.SetKind(ptrace.SpanKindClient)
		s.Attributes().PutStr("gen_ai.provider.name", "openai")
	})
	if len(got) != 1 || got[0].Attribute != "gen_ai.operation.name" {
		t.Errorf("want one finding about gen_ai.operation.name, got %v", got)
	}
}

func TestSpanNameFallbackWithoutModel(t *testing.T) {
	bare := evalOne(t, newSpanName, func(s ptrace.Span) {
		s.SetName("chat")
		s.SetKind(ptrace.SpanKindClient)
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.provider.name", "openai")
	})
	if len(bare) != 0 {
		t.Errorf("bare operation name is correct when the model attr is absent, got %v", bare)
	}

	wrong := evalOne(t, newSpanName, func(s ptrace.Span) {
		s.SetName("chat")
		s.SetKind(ptrace.SpanKindClient)
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.request.model", "gpt-4")
	})
	if len(wrong) != 1 || !strings.Contains(wrong[0].Message, `"chat gpt-4"`) {
		t.Errorf(`span with model attr must be named "chat gpt-4", got %v`, wrong)
	}
}

func TestIntToleratedForDoubleAttributes(t *testing.T) {
	got := evalOne(t, newSpanAttrTypes, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutInt("gen_ai.request.temperature", 1)
	})
	if len(got) != 0 {
		t.Errorf("int for double must be tolerated, got %v", got)
	}

	bad := evalOne(t, newSpanAttrTypes, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.usage.input_tokens", "1200")
	})
	if len(bad) != 1 || bad[0].Attribute != "gen_ai.usage.input_tokens" {
		t.Errorf("string token count must be a type finding, got %v", bad)
	}
}

func TestTemplateAttributesTypeChecked(t *testing.T) {
	ok := evalOne(t, newSpanAttrTypes, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.prompt.variable.user_name", "Alice")
	})
	if len(ok) != 0 {
		t.Errorf("string template attribute is fine, got %v", ok)
	}

	bad := evalOne(t, newSpanAttrTypes, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutInt("gen_ai.prompt.variable.count", 3)
	})
	if len(bad) != 1 {
		t.Errorf("non-string template attribute must be flagged, got %v", bad)
	}
}

func TestClosedVersusOpenEnums(t *testing.T) {
	closed := evalOne(t, newSpanClosedEnums, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.output.type", "binary")
	})
	if len(closed) != 1 || closed[0].Severity != engine.SeverityError {
		t.Errorf("unknown closed-enum value must be an ERROR, got %v", closed)
	}

	open := evalOne(t, newSpanOpenEnums, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.provider.name", "my-in-house-llm")
	})
	if len(open) != 1 || open[0].Severity != engine.SeverityInfo {
		t.Errorf("custom open-enum value must be an INFO hint, got %v", open)
	}
}

func TestUnknownAttrSkipsDeprecated(t *testing.T) {
	got := evalOne(t, newSpanUnknownAttrs, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("gen_ai.system", "openai")         // deprecated: GENAI-SPAN-009's job
		s.Attributes().PutInt("gen_ai.usage.total_tokens", 1250) // unknown: this rule's job
	})
	if len(got) != 1 || got[0].Attribute != "gen_ai.usage.total_tokens" {
		t.Errorf("only the unknown attribute must be flagged, got %v", got)
	}
}

func TestSpanNameDoesNotLoopOnSelfReferentialValue(t *testing.T) {
	// A crafted attribute value that contains its own placeholder must not
	// send expectedSpanName into an infinite loop (a single such span would
	// otherwise wedge the ingest goroutine). The check must terminate fast
	// and simply report a name mismatch.
	done := make(chan []engine.Finding, 1)
	go func() {
		done <- evalOne(t, newSpanName, func(s ptrace.Span) {
			s.SetName("chat gpt-4")
			s.SetKind(ptrace.SpanKindClient)
			s.Attributes().PutStr("gen_ai.operation.name", "chat")
			s.Attributes().PutStr("gen_ai.request.model", "{gen_ai.request.model}")
		})
	}()
	select {
	case got := <-done:
		// Expected name resolves to the literal value once, no rescan.
		if len(got) != 1 {
			t.Errorf("want one span-name finding, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expectedSpanName did not terminate: self-referential attribute value looped")
	}
}

func TestErrorTypeRequiredOnErrorStatus(t *testing.T) {
	missing := evalOne(t, newSpanErrorType, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Status().SetCode(ptrace.StatusCodeError)
	})
	if len(missing) != 1 {
		t.Errorf("error status without error.type must be flagged, got %v", missing)
	}

	present := evalOne(t, newSpanErrorType, func(s ptrace.Span) {
		s.Attributes().PutStr("gen_ai.operation.name", "chat")
		s.Attributes().PutStr("error.type", "timeout")
		s.Status().SetCode(ptrace.StatusCodeError)
	})
	if len(present) != 0 {
		t.Errorf("error.type present must pass, got %v", present)
	}
}
