package assembly

import (
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

type capture struct {
	traces  []*engine.TraceContext
	reasons []CloseReason
}

func (c *capture) onClose(tc *engine.TraceContext, reason CloseReason) {
	c.traces = append(c.traces, tc)
	c.reasons = append(c.reasons, reason)
}

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time          { return f.now }
func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

func batchWith(traceID byte, spanID byte) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "svc")
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID([16]byte{traceID, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}))
	span.SetSpanID(pcommon.SpanID([8]byte{spanID, 1, 2, 3, 4, 5, 6, 7}))
	span.SetName("chat gpt-4")
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	return td
}

func newTestStore(cfg Config) (*Store, *capture, *fakeClock) {
	rec := &capture{}
	clock := &fakeClock{now: time.Unix(1_751_500_000, 0)}
	s := New(cfg, registry.Default(), rec.onClose)
	s.SetClock(clock.Now)
	return s, rec, clock
}

func TestIdleTimeoutClosesWindow(t *testing.T) {
	s, rec, clock := newTestStore(Config{IdleTimeout: 10 * time.Second, MaxAge: time.Minute})

	s.AddBatch(batchWith(1, 1))
	clock.Advance(5 * time.Second)
	s.AddBatch(batchWith(1, 2)) // activity resets idle
	clock.Advance(9 * time.Second)
	if s.Sweep() != 0 {
		t.Fatal("window closed while still within idle timeout")
	}
	clock.Advance(2 * time.Second)
	if s.Sweep() != 1 {
		t.Fatal("idle window not closed")
	}
	if len(rec.traces) != 1 || len(rec.traces[0].Spans) != 2 || rec.reasons[0] != CloseIdle {
		t.Fatalf("unexpected close: %+v %v", rec.traces, rec.reasons)
	}
}

func TestMaxAgeClosesActiveWindow(t *testing.T) {
	s, rec, clock := newTestStore(Config{IdleTimeout: 10 * time.Second, MaxAge: 30 * time.Second})

	s.AddBatch(batchWith(1, 1))
	for i := 0; i < 6; i++ { // keep it active past max age
		clock.Advance(5 * time.Second)
		s.AddBatch(batchWith(1, byte(2+i)))
		s.Sweep()
	}
	if len(rec.traces) != 1 || rec.reasons[0] != CloseMaxAge {
		t.Fatalf("active window must close at max age: %v", rec.reasons)
	}
}

func TestLRUEvictionMarksIncomplete(t *testing.T) {
	s, rec, _ := newTestStore(Config{MaxTraces: 2})

	s.AddBatch(batchWith(1, 1))
	s.AddBatch(batchWith(2, 1))
	s.AddBatch(batchWith(3, 1)) // evicts trace 1 (least recently active)

	if len(rec.traces) != 1 || rec.reasons[0] != CloseEvicted {
		t.Fatalf("expected one eviction, got %v", rec.reasons)
	}
	if !rec.traces[0].Incomplete {
		t.Error("evicted trace must be marked Incomplete")
	}
	if got := s.Stats().TracesOpen; got != 2 {
		t.Errorf("open traces = %d, want 2", got)
	}
}

func TestSpanCapTruncates(t *testing.T) {
	s, rec, _ := newTestStore(Config{MaxSpansPerTrace: 2})

	for i := 0; i < 4; i++ {
		s.AddBatch(batchWith(1, byte(i+1)))
	}
	s.CloseAll()
	if s.Stats().SpansReceived != 4 {
		t.Errorf("received = %d, want 4", s.Stats().SpansReceived)
	}
	if len(rec.traces) != 1 {
		t.Fatalf("closes = %d, want 1", len(rec.traces))
	}
	if got := len(rec.traces[0].Spans); got != 2 {
		t.Errorf("kept spans = %d, want 2 (the cap)", got)
	}
	if !rec.traces[0].Incomplete {
		t.Error("truncated trace must be marked Incomplete")
	}
}

func TestLateArrivalStartsLateWindow(t *testing.T) {
	s, rec, clock := newTestStore(Config{IdleTimeout: 5 * time.Second, MaxAge: time.Minute})

	s.AddBatch(batchWith(1, 1))
	clock.Advance(6 * time.Second)
	s.Sweep() // closes trace 1

	s.AddBatch(batchWith(1, 9)) // same trace ID, after close
	clock.Advance(6 * time.Second)
	s.Sweep()

	if len(rec.traces) != 2 {
		t.Fatalf("closes = %d, want 2", len(rec.traces))
	}
	if rec.traces[0].LateArrival {
		t.Error("first window must not be late")
	}
	if !rec.traces[1].LateArrival {
		t.Error("second window must be marked LateArrival")
	}
	if s.Stats().LateSpans != 1 {
		t.Errorf("late spans = %d, want 1", s.Stats().LateSpans)
	}
}

func emptyTraceBatch(service string, spanID byte) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", service)
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	// Trace ID left empty on purpose.
	span.SetSpanID(pcommon.SpanID([8]byte{spanID, 1, 2, 3, 4, 5, 6, 7}))
	span.SetName("chat gpt-4")
	span.Attributes().PutStr("gen_ai.operation.name", "chat")
	return td
}

func TestEmptyTraceIDsAreNotAssembled(t *testing.T) {
	s, rec, _ := newTestStore(Config{})
	s.AddBatch(emptyTraceBatch("svc-a", 1))
	s.AddBatch(emptyTraceBatch("svc-b", 2))
	s.CloseAll()
	// Neither span should have opened a window (they cannot participate in
	// topology); crucially they must not merge into one shared "" trace.
	if len(rec.traces) != 0 {
		t.Errorf("empty-trace-id spans must not form windows, got %d", len(rec.traces))
	}
	if s.Stats().SpansReceived != 2 {
		t.Errorf("received = %d, want 2", s.Stats().SpansReceived)
	}
}

func TestEvictionDoesNotMarkLaterSpansLate(t *testing.T) {
	s, rec, _ := newTestStore(Config{MaxTraces: 1})
	s.AddBatch(batchWith(1, 1)) // opens trace 1
	s.AddBatch(batchWith(2, 1)) // evicts trace 1
	s.AddBatch(batchWith(1, 2)) // trace 1's late span — must NOT be "late"
	s.CloseAll()

	for _, tc := range rec.traces {
		if tc.LateArrival {
			t.Errorf("eviction must not make a trace's own later spans a late arrival: %+v", tc)
		}
	}
	if s.Stats().LateSpans != 0 {
		t.Errorf("late spans after eviction = %d, want 0", s.Stats().LateSpans)
	}
}

func TestLateSpansCountedPerSpan(t *testing.T) {
	s, _, clock := newTestStore(Config{IdleTimeout: 5 * time.Second, MaxAge: time.Minute})
	s.AddBatch(batchWith(1, 1))
	clock.Advance(6 * time.Second)
	s.Sweep() // closes trace 1 (judged)

	s.AddBatch(batchWith(1, 2)) // late span 1
	s.AddBatch(batchWith(1, 3)) // late span 2 — same reopened window
	if got := s.Stats().LateSpans; got != 2 {
		t.Errorf("late spans = %d, want 2 (every late span, not just the first)", got)
	}
}

func TestResetDropsOpenWindows(t *testing.T) {
	s, rec, _ := newTestStore(Config{})
	s.AddBatch(batchWith(1, 1))
	s.AddBatch(batchWith(2, 1))
	s.Reset()
	if s.Stats().TracesOpen != 0 || s.Stats().SpansBuffered != 0 {
		t.Errorf("Reset must drop open windows, got %+v", s.Stats())
	}
	// The dropped windows must never reach the callback later.
	s.CloseAll()
	if len(rec.traces) != 0 {
		t.Errorf("reset windows must not be judged, got %d closes", len(rec.traces))
	}
}

func TestCloseAllFlushesEverything(t *testing.T) {
	s, rec, _ := newTestStore(Config{})
	s.AddBatch(batchWith(1, 1))
	s.AddBatch(batchWith(2, 1))
	s.CloseAll()
	if len(rec.traces) != 2 {
		t.Fatalf("closed = %d, want 2", len(rec.traces))
	}
	for _, r := range rec.reasons {
		if r != CloseShutdown {
			t.Errorf("reason = %v, want shutdown", r)
		}
	}
	if s.Stats().TracesOpen != 0 || s.Stats().SpansBuffered != 0 {
		t.Errorf("store not empty after CloseAll: %+v", s.Stats())
	}
}
