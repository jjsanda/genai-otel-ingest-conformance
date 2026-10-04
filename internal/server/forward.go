package server

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	forwardTimeout    = 5 * time.Second
	forwardQueueDepth = 1024
)

// forwarder optionally passes validated telemetry to a downstream OTLP gRPC
// endpoint so the gateway can sit in-line in an existing pipeline.
//
// Forwarding is best-effort and off the ingest hot path: exports run on a
// single background worker draining a bounded queue, so a slow or down
// downstream never blocks the OTLP receivers (conformance checking must
// never become the reason ingest stalls or telemetry is lost). When the
// queue is full, batches are dropped and counted rather than applying
// backpressure. Plaintext gRPC only — front it with a TLS-terminating
// collector for anything beyond demo scale.
type forwarder struct {
	log     *slog.Logger
	traces  ptraceotlp.GRPCClient
	metrics pmetricotlp.GRPCClient
	logs    plogotlp.GRPCClient

	queue   chan func()
	dropped func() // called when a batch is dropped (queue full)
}

func newForwarder(endpoint string, log *slog.Logger, onDrop func()) (*forwarder, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	f := &forwarder{
		log:     log,
		traces:  ptraceotlp.NewGRPCClient(conn),
		metrics: pmetricotlp.NewGRPCClient(conn),
		logs:    plogotlp.NewGRPCClient(conn),
		queue:   make(chan func(), forwardQueueDepth),
		dropped: onDrop,
	}
	go f.run()
	return f, nil
}

// run drains the queue on a single goroutine; exports are serialized, which
// preserves rough ordering and bounds concurrency at one.
func (f *forwarder) run() {
	for job := range f.queue {
		job()
	}
}

// enqueue submits a job without blocking; a full queue drops the batch.
func (f *forwarder) enqueue(job func()) {
	select {
	case f.queue <- job:
	default:
		if f.dropped != nil {
			f.dropped()
		}
		f.log.Warn("forward queue full, dropping batch")
	}
}

// Stop drains and stops the worker.
func (f *forwarder) Stop() { close(f.queue) }

func (f *forwarder) forwardTraces(td ptrace.Traces) {
	f.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), forwardTimeout)
		defer cancel()
		if _, err := f.traces.Export(ctx, ptraceotlp.NewExportRequestFromTraces(td)); err != nil {
			f.log.Warn("forwarding traces failed", "error", err)
		}
	})
}

func (f *forwarder) forwardMetrics(md pmetric.Metrics) {
	f.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), forwardTimeout)
		defer cancel()
		if _, err := f.metrics.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(md)); err != nil {
			f.log.Warn("forwarding metrics failed", "error", err)
		}
	})
}

func (f *forwarder) forwardLogs(ld plog.Logs) {
	f.enqueue(func() {
		ctx, cancel := context.WithTimeout(context.Background(), forwardTimeout)
		defer cancel()
		if _, err := f.logs.Export(ctx, plogotlp.NewExportRequestFromLogs(ld)); err != nil {
			f.log.Warn("forwarding logs failed", "error", err)
		}
	})
}
