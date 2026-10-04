package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/assembly"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/server"
)

const serveUsage = `Run the OTLP ingest gateway with live conformance checking.

Receives OTLP traces/metrics/logs, evaluates them against the GenAI
semantic-conventions rule catalog, and serves a live report, score badge,
and Prometheus self-metrics on the admin port.

Usage:
  genai-conformance serve [flags]

Flags:
`

func runServe(args []string, _, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	grpcAddr := fs.String("grpc", ":4317", "OTLP gRPC listen address")
	httpAddr := fs.String("http", ":4318", "OTLP HTTP listen address")
	adminAddr := fs.String("admin", ":8080", "admin listen address (report UI, /api/report, /metrics, health)")
	windowIdle := fs.Duration("window-idle", 10*time.Second, "close a trace window after this long without new spans")
	windowMaxAge := fs.Duration("window-max-age", 60*time.Second, "close a trace window this long after its first span, regardless of activity")
	maxTraces := fs.Int("max-traces", 10_000, "maximum open trace windows before LRU eviction")
	maxSpans := fs.Int("max-spans-per-trace", 2_000, "maximum spans buffered per trace window")
	forward := fs.String("forward", "", "optional downstream OTLP gRPC endpoint to forward all telemetry to (plaintext)")
	fs.Usage = func() {
		fmt.Fprint(stderr, serveUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "genai-conformance serve: unexpected arguments %v\n", fs.Args())
		return ExitUsage
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	srv, err := server.New(server.Config{
		GRPCAddr:  *grpcAddr,
		HTTPAddr:  *httpAddr,
		AdminAddr: *adminAddr,
		Window: assembly.Config{
			IdleTimeout:      *windowIdle,
			MaxAge:           *windowMaxAge,
			MaxTraces:        *maxTraces,
			MaxSpansPerTrace: *maxSpans,
		},
		Forward: *forward,
		Version: Version,
		Logger:  logger,
	})
	if err != nil {
		fmt.Fprintf(stderr, "genai-conformance serve: %v\n", err)
		return ExitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(stderr, "genai-conformance serve: %v\n", err)
		return 1
	}
	return ExitOK
}
