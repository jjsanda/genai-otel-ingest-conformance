// Package server composes the conformance gateway: OTLP receivers feeding
// the engine, the bounded trace-window store, the live report, Prometheus
// self-metrics, the admin/UI endpoints, and graceful lifecycle.
package server

import (
	"log/slog"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/assembly"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/report"
)

// handler is the ingest.Consumer: every received batch is evaluated
// streaming (span/resource/metric/log rules), aggregated into the live
// report and self-metrics, buffered for trace assembly, and optionally
// forwarded downstream.
type handler struct {
	log     *slog.Logger
	eng     *engine.Engine
	live    *report.Live
	store   *assembly.Store
	metrics *promMetrics
	fwd     *forwarder
}

func (h *handler) ConsumeTraces(td ptrace.Traces) {
	h.metrics.spansReceived.Add(float64(td.SpanCount()))
	res := h.eng.EvaluateTraces(td)
	h.record(res)
	h.store.AddBatch(td)
	if h.fwd != nil {
		h.fwd.forwardTraces(td)
	}
}

func (h *handler) ConsumeMetrics(md pmetric.Metrics) {
	h.metrics.pointsReceived.Add(float64(md.DataPointCount()))
	h.record(h.eng.EvaluateMetrics(md))
	if h.fwd != nil {
		h.fwd.forwardMetrics(md)
	}
}

func (h *handler) ConsumeLogs(ld plog.Logs) {
	h.metrics.recordsReceived.Add(float64(ld.LogRecordCount()))
	h.record(h.eng.EvaluateLogs(ld))
	if h.fwd != nil {
		h.fwd.forwardLogs(ld)
	}
}

// onWindowClose evaluates an assembled trace when its window closes.
func (h *handler) onWindowClose(tc *engine.TraceContext, reason assembly.CloseReason) {
	h.metrics.windowsClosed.WithLabelValues(string(reason)).Inc()
	h.record(h.eng.EvaluateTrace(tc))
}

func (h *handler) record(res *engine.Result) {
	h.live.Add(res)
	h.metrics.recordResult(res)
	h.metrics.updateScores(h.live)
}
