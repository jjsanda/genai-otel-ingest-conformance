package server

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/assembly"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/report"
)

// promMetrics is the gateway's self-observability surface — the numbers the
// provisioned Grafana dashboard is built on.
type promMetrics struct {
	spansReceived   prometheus.Counter
	pointsReceived  prometheus.Counter
	recordsReceived prometheus.Counter
	findings        *prometheus.CounterVec
	checks          *prometheus.CounterVec
	passed          *prometheus.CounterVec
	score           *prometheus.GaugeVec
	windowsClosed   *prometheus.CounterVec
	forwardDropped  prometheus.Counter
}

func newPromMetrics(reg *prometheus.Registry, store *assembly.Store, version, semconvSHA string) *promMetrics {
	m := &promMetrics{
		spansReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "genai_conformance_spans_received_total",
			Help: "Spans received across OTLP receivers.",
		}),
		pointsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "genai_conformance_metric_points_received_total",
			Help: "Metric data points received across OTLP receivers.",
		}),
		recordsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "genai_conformance_log_records_received_total",
			Help: "Log records received across OTLP receivers.",
		}),
		findings: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "genai_conformance_findings_total",
			Help: "Conformance findings by rule, severity, and service.",
		}, []string{"rule_id", "severity", "service"}),
		checks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "genai_conformance_checks_total",
			Help: "Scoreable rule evaluations by service.",
		}, []string{"service"}),
		passed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "genai_conformance_checks_passed_total",
			Help: "Passed rule evaluations by service.",
		}, []string{"service"}),
		score: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "genai_conformance_score",
			Help: "Current conformance score (0..1) by service.",
		}, []string{"service"}),
		windowsClosed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "genai_conformance_windows_closed_total",
			Help: "Trace windows closed, by reason (idle, max_age, evicted, shutdown).",
		}, []string{"reason"}),
		forwardDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "genai_conformance_forward_dropped_total",
			Help: "Telemetry batches dropped because the forward queue was full.",
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "genai_conformance_build_info",
		Help: "Build and semconv-pin metadata (value is always 1).",
	}, []string{"version", "semconv_sha"})
	buildInfo.WithLabelValues(version, semconvSHA).Set(1)

	reg.MustRegister(
		m.spansReceived, m.pointsReceived, m.recordsReceived,
		m.findings, m.checks, m.passed, m.score, m.windowsClosed,
		m.forwardDropped, buildInfo, &storeCollector{store: store},
	)
	return m
}

func (m *promMetrics) recordResult(res *engine.Result) {
	for _, f := range res.Findings {
		m.findings.WithLabelValues(f.RuleID, string(f.Severity), f.Service).Inc()
	}
	for svc, t := range res.Tallies {
		m.checks.WithLabelValues(svc).Add(float64(t.Checks))
		m.passed.WithLabelValues(svc).Add(float64(t.Passed))
	}
}

func (m *promMetrics) updateScores(live *report.Live) {
	for svc, score := range live.ServiceScores() {
		m.score.WithLabelValues(svc).Set(score)
	}
}

// storeCollector exposes the window store's internal counters without the
// store knowing about Prometheus.
type storeCollector struct {
	store *assembly.Store
}

var (
	descTracesOpen = prometheus.NewDesc("genai_conformance_traces_open",
		"Trace windows currently open.", nil, nil)
	descSpansBuffered = prometheus.NewDesc("genai_conformance_spans_buffered",
		"Spans buffered in open trace windows.", nil, nil)
	descLateSpans = prometheus.NewDesc("genai_conformance_late_spans_total",
		"Spans that arrived after their trace window had closed.", nil, nil)
)

func (c *storeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descTracesOpen
	ch <- descSpansBuffered
	ch <- descLateSpans
}

func (c *storeCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.store.Stats()
	ch <- prometheus.MustNewConstMetric(descTracesOpen, prometheus.GaugeValue, float64(stats.TracesOpen))
	ch <- prometheus.MustNewConstMetric(descSpansBuffered, prometheus.GaugeValue, float64(stats.SpansBuffered))
	ch <- prometheus.MustNewConstMetric(descLateSpans, prometheus.CounterValue, float64(stats.LateSpans))
}
