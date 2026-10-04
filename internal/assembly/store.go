// Package assembly implements the gateway's bounded trace-window store.
//
// Span- and resource-level rules run streaming at ingest; trace-topology
// rules need whole traces, and OTLP delivers spans in arbitrary batches. The
// store groups spans by trace ID into windows and closes a window when it has
// been idle (no new spans) or too old, handing the assembled trace to a
// callback for evaluation.
//
// Bounds are explicit (ADR-0004): open windows are capped with LRU eviction,
// spans per trace are capped with truncation, and both mark the trace
// Incomplete so topology rules soften instead of false-positive. Late spans
// never reopen a closed window: they start a fresh window marked LateArrival,
// which trace rules surface as a finding — late telemetry is itself a signal
// of mis-tuned batching upstream.
//
// Concurrency is a single mutex around a map plus an LRU list, swept by a 1s
// ticker. At demo-gateway scale that is the honest design; sharding would be
// speculative complexity.
package assembly

import (
	"container/list"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
)

// CloseReason explains why a window was closed.
type CloseReason string

// Window close reasons, exported as the reason label on the
// genai_conformance_windows_closed_total counter.
const (
	CloseIdle     CloseReason = "idle"
	CloseMaxAge   CloseReason = "max_age"
	CloseEvicted  CloseReason = "evicted"
	CloseShutdown CloseReason = "shutdown"
)

// Config bounds the store.
type Config struct {
	// IdleTimeout closes a window that received no span for this long.
	IdleTimeout time.Duration
	// MaxAge closes a window this long after its first span regardless of
	// activity, bounding worst-case report latency.
	MaxAge time.Duration
	// MaxTraces caps open windows; beyond it the least-recently-active
	// window is evicted (closed early, marked Incomplete).
	MaxTraces int
	// MaxSpansPerTrace caps spans kept per window; excess spans are dropped
	// and the window marked Incomplete.
	MaxSpansPerTrace int
	// ClosedRetention is how long closed trace IDs are remembered to detect
	// late arrivals; it also bounds the memory of that memory.
	ClosedRetention time.Duration
	// MaxClosedIDs caps the late-detection set.
	MaxClosedIDs int
}

// Defaults returns the production defaults documented in ADR-0004.
func Defaults() Config {
	return Config{
		IdleTimeout:      10 * time.Second,
		MaxAge:           60 * time.Second,
		MaxTraces:        10_000,
		MaxSpansPerTrace: 2_000,
		ClosedRetention:  2 * time.Minute,
		MaxClosedIDs:     50_000,
	}
}

func (c Config) withDefaults() Config {
	d := Defaults()
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = d.IdleTimeout
	}
	if c.MaxAge <= 0 {
		c.MaxAge = d.MaxAge
	}
	if c.MaxTraces <= 0 {
		c.MaxTraces = d.MaxTraces
	}
	if c.MaxSpansPerTrace <= 0 {
		c.MaxSpansPerTrace = d.MaxSpansPerTrace
	}
	if c.ClosedRetention <= 0 {
		c.ClosedRetention = d.ClosedRetention
	}
	if c.MaxClosedIDs <= 0 {
		c.MaxClosedIDs = d.MaxClosedIDs
	}
	return c
}

// Stats is a point-in-time snapshot of store counters.
type Stats struct {
	TracesOpen    int
	SpansBuffered int
	SpansReceived uint64
	LateSpans     uint64
	Closed        map[CloseReason]uint64
}

type window struct {
	tc        *engine.TraceContext
	firstSeen time.Time
	lastSeen  time.Time
	lruEl     *list.Element
}

// Store assembles spans into trace windows. All methods are safe for
// concurrent use.
type Store struct {
	cfg     Config
	reg     *registry.Registry
	onClose func(*engine.TraceContext, CloseReason)
	clock   func() time.Time

	mu       sync.Mutex
	windows  map[string]*window
	lru      *list.List // element value: trace ID string; front = most recent
	closed   map[string]time.Time
	closedQ  *list.List // trace IDs in close order, for retention trimming
	received uint64
	late     uint64
	spansBuf int
	closedBy map[CloseReason]uint64
}

// New builds a store; onClose receives each assembled trace exactly once.
func New(cfg Config, reg *registry.Registry, onClose func(*engine.TraceContext, CloseReason)) *Store {
	return &Store{
		cfg:      cfg.withDefaults(),
		reg:      reg,
		onClose:  onClose,
		clock:    time.Now,
		windows:  map[string]*window{},
		lru:      list.New(),
		closed:   map[string]time.Time{},
		closedQ:  list.New(),
		closedBy: map[CloseReason]uint64{},
	}
}

// SetClock injects a fake clock for tests.
func (s *Store) SetClock(clock func() time.Time) { s.clock = clock }

// AddBatch feeds every span of a trace batch into the store.
func (s *Store) AddBatch(td ptrace.Traces) {
	now := s.clock()
	var evict []*window

	s.mu.Lock()
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		service := resourceService(rs.Resource())
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				evict = append(evict, s.addLocked(rs.Resource(), ss.Scope(), span, service, now)...)
			}
		}
	}
	s.mu.Unlock()

	for _, w := range evict {
		s.onClose(w.tc, CloseEvicted)
	}
}

// addLocked files one span and returns any windows evicted to make room.
func (s *Store) addLocked(res pcommon.Resource, scope pcommon.InstrumentationScope, span ptrace.Span, service string, now time.Time) []*window {
	s.received++
	// A span with no trace ID cannot participate in trace assembly, and
	// pcommon.TraceID.String() renders every empty ID as "" — keying on it
	// would collapse zero-ID spans from every service into one shared
	// window and manufacture cross-service topology findings. Span- and
	// resource-level rules already judged this span streaming; drop it here.
	if span.TraceID().IsEmpty() {
		return nil
	}
	id := span.TraceID().String()

	w, ok := s.windows[id]
	if !ok {
		_, wasClosed := s.closed[id]
		w = &window{
			tc: &engine.TraceContext{
				Registry:    s.reg,
				TraceID:     id,
				ByID:        map[string]*engine.SpanContext{},
				LateArrival: wasClosed,
			},
			firstSeen: now,
		}
		w.lruEl = s.lru.PushFront(id)
		s.windows[id] = w
	}
	// Count every span arriving for a trace whose window already closed and
	// was judged, not only the one that reopened it — the metric is
	// genai_conformance_late_spans_total.
	if w.tc.LateArrival {
		s.late++
	}

	if len(w.tc.Spans) >= s.cfg.MaxSpansPerTrace {
		w.tc.Incomplete = true
	} else {
		sctx := engine.NewSpanContext(s.reg, res, scope, span, service)
		w.tc.Spans = append(w.tc.Spans, sctx)
		w.tc.ByID[span.SpanID().String()] = sctx
		s.spansBuf++
	}
	w.lastSeen = now
	s.lru.MoveToFront(w.lruEl)

	var evicted []*window
	for len(s.windows) > s.cfg.MaxTraces {
		oldest := s.lru.Back()
		if oldest == nil {
			break
		}
		ow := s.windows[oldest.Value.(string)]
		ow.tc.Incomplete = true
		s.closeLocked(ow, CloseEvicted, now)
		evicted = append(evicted, ow)
	}
	return evicted
}

// Sweep closes idle and over-age windows; call it from a ticker. It returns
// the number of windows closed.
func (s *Store) Sweep() int {
	now := s.clock()
	var due []*window
	var reasons []CloseReason

	s.mu.Lock()
	for _, w := range s.windows {
		switch {
		case now.Sub(w.firstSeen) >= s.cfg.MaxAge:
			due = append(due, w)
			reasons = append(reasons, CloseMaxAge)
		case now.Sub(w.lastSeen) >= s.cfg.IdleTimeout:
			due = append(due, w)
			reasons = append(reasons, CloseIdle)
		}
	}
	for i, w := range due {
		s.closeLocked(w, reasons[i], now)
	}
	s.trimClosedLocked(now)
	s.mu.Unlock()

	for i, w := range due {
		s.onClose(w.tc, reasons[i])
	}
	return len(due)
}

// CloseAll flushes every open window (graceful shutdown).
func (s *Store) CloseAll() {
	now := s.clock()
	s.mu.Lock()
	var all []*window
	for _, w := range s.windows {
		all = append(all, w)
	}
	for _, w := range all {
		s.closeLocked(w, CloseShutdown, now)
	}
	s.mu.Unlock()

	for _, w := range all {
		s.onClose(w.tc, CloseShutdown)
	}
}

// closeLocked removes the window from all indexes and records its ID for
// late-arrival detection. The onClose callback runs outside the lock.
func (s *Store) closeLocked(w *window, reason CloseReason, now time.Time) {
	delete(s.windows, w.tc.TraceID)
	s.lru.Remove(w.lruEl)
	s.spansBuf -= len(w.tc.Spans)
	s.closedBy[reason]++

	// Only closes that mean "the trace was fully judged" make later spans
	// genuinely late. Eviction is a capacity drop while spans may still be
	// arriving; recording it here would make the trace's own remaining
	// spans look like a late arrival and trip GENAI-TRACE-005 falsely.
	if reason == CloseEvicted {
		return
	}
	if _, dup := s.closed[w.tc.TraceID]; !dup {
		s.closed[w.tc.TraceID] = now
		s.closedQ.PushBack(w.tc.TraceID)
	}
	for s.closedQ.Len() > s.cfg.MaxClosedIDs {
		el := s.closedQ.Front()
		s.closedQ.Remove(el)
		delete(s.closed, el.Value.(string))
	}
}

func (s *Store) trimClosedLocked(now time.Time) {
	for el := s.closedQ.Front(); el != nil; {
		id := el.Value.(string)
		if now.Sub(s.closed[id]) < s.cfg.ClosedRetention {
			break
		}
		next := el.Next()
		s.closedQ.Remove(el)
		delete(s.closed, id)
		el = next
	}
}

// Reset drops all open windows and the late-arrival memory without judging
// the buffered spans. It backs the gateway's /api/reset: without it, windows
// opened before a reset would close later and inject their findings into the
// supposedly fresh report. Lifetime counters (received, late, closes) are
// left intact so the Prometheus counters stay monotonic.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.windows = map[string]*window{}
	s.lru.Init()
	s.closed = map[string]time.Time{}
	s.closedQ.Init()
	s.spansBuf = 0
}

// Stats snapshots the counters.
func (s *Store) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	closed := make(map[CloseReason]uint64, len(s.closedBy))
	for k, v := range s.closedBy {
		closed[k] = v
	}
	return Stats{
		TracesOpen:    len(s.windows),
		SpansBuffered: s.spansBuf,
		SpansReceived: s.received,
		LateSpans:     s.late,
		Closed:        closed,
	}
}

func resourceService(res pcommon.Resource) string {
	if v, ok := res.Attributes().Get("service.name"); ok && v.Str() != "" {
		return v.Str()
	}
	return "unknown_service"
}
