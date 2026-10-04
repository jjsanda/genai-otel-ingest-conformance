// Package ingest implements the gateway's OTLP receivers (gRPC and HTTP) on
// top of the collector's pdata wire types. Receivers decode and hand batches
// to a Consumer; they never block on evaluation details.
package ingest

import (
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// Consumer receives decoded OTLP batches from the receivers.
type Consumer interface {
	ConsumeTraces(td ptrace.Traces)
	ConsumeMetrics(md pmetric.Metrics)
	ConsumeLogs(ld plog.Logs)
}
