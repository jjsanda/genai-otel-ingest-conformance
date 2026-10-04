package ingest

import (
	"context"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"

	// Register the gzip codec: OTLP senders (the collector included)
	// compress by default, and a server without the decompressor rejects
	// every export with "Decompressor is not installed".
	_ "google.golang.org/grpc/encoding/gzip"
)

const maxRecvBytes = 32 * 1024 * 1024

// NewGRPCServer builds a gRPC server exposing the three OTLP export
// services.
func NewGRPCServer(c Consumer) *grpc.Server {
	srv := grpc.NewServer(grpc.MaxRecvMsgSize(maxRecvBytes))
	ptraceotlp.RegisterGRPCServer(srv, &traceService{consumer: c})
	pmetricotlp.RegisterGRPCServer(srv, &metricService{consumer: c})
	plogotlp.RegisterGRPCServer(srv, &logService{consumer: c})
	return srv
}

type traceService struct {
	ptraceotlp.UnimplementedGRPCServer
	consumer Consumer
}

func (s *traceService) Export(_ context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	s.consumer.ConsumeTraces(req.Traces())
	return ptraceotlp.NewExportResponse(), nil
}

type metricService struct {
	pmetricotlp.UnimplementedGRPCServer
	consumer Consumer
}

func (s *metricService) Export(_ context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	s.consumer.ConsumeMetrics(req.Metrics())
	return pmetricotlp.NewExportResponse(), nil
}

type logService struct {
	plogotlp.UnimplementedGRPCServer
	consumer Consumer
}

func (s *logService) Export(_ context.Context, req plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	s.consumer.ConsumeLogs(req.Logs())
	return plogotlp.NewExportResponse(), nil
}
