package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/assembly"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/ingest"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/report"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/rules"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/webui"
)

const sweepInterval = time.Second

// Config configures the gateway.
type Config struct {
	GRPCAddr  string
	HTTPAddr  string
	AdminAddr string
	Window    assembly.Config
	// Forward optionally names a downstream OTLP gRPC endpoint for
	// pass-through operation.
	Forward string
	Version string
	Logger  *slog.Logger
}

// Server is the conformance gateway.
type Server struct {
	cfg  Config
	log  *slog.Logger
	live *report.Live

	grpcSrv  *grpc.Server
	httpSrv  *http.Server
	adminSrv *http.Server
	store    *assembly.Store
	fwd      *forwarder

	grpcAddr  atomic.Value // string
	httpAddr  atomic.Value
	adminAddr atomic.Value
	ready     atomic.Bool
}

// New wires the gateway together.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	reg := registry.Default()
	eng := engine.New(reg, rules.All(reg)...)
	live := report.NewLive(reg, eng.Rules(), cfg.Version)

	h := &handler{log: cfg.Logger, eng: eng, live: live}

	promReg := prometheus.NewRegistry()
	h.store = nil // set below once metrics exist (collector needs the store)
	store := assembly.New(cfg.Window, reg, h.onWindowClose)
	h.store = store
	h.metrics = newPromMetrics(promReg, store, cfg.Version, reg.Meta.SHA)

	s := &Server{cfg: cfg, log: cfg.Logger, live: live, store: store}
	if cfg.Forward != "" {
		fwd, err := newForwarder(cfg.Forward, cfg.Logger, h.metrics.forwardDropped.Inc)
		if err != nil {
			return nil, fmt.Errorf("configuring forwarder to %s: %w", cfg.Forward, err)
		}
		h.fwd = fwd
		s.fwd = fwd
	}
	s.grpcSrv = ingest.NewGRPCServer(h)
	s.httpSrv = &http.Server{Handler: ingest.NewHTTPHandler(h), ReadHeaderTimeout: 10 * time.Second}
	s.adminSrv = &http.Server{Handler: s.adminMux(promReg), ReadHeaderTimeout: 10 * time.Second}
	return s, nil
}

// GRPCAddr returns the bound OTLP gRPC address once Run has started.
func (s *Server) GRPCAddr() string { v, _ := s.grpcAddr.Load().(string); return v }

// HTTPAddr returns the bound OTLP HTTP address once Run has started.
func (s *Server) HTTPAddr() string { v, _ := s.httpAddr.Load().(string); return v }

// AdminAddr returns the bound admin address once Run has started.
func (s *Server) AdminAddr() string { v, _ := s.adminAddr.Load().(string); return v }

// Run serves until ctx is canceled, then shuts down gracefully: receivers
// stop first, remaining trace windows are flushed and judged, and the admin
// endpoint goes last so the final report stays observable during drain.
func (s *Server) Run(ctx context.Context) error {
	grpcLn, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listening on gRPC addr: %w", err)
	}
	httpLn, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listening on HTTP addr: %w", err)
	}
	adminLn, err := net.Listen("tcp", s.cfg.AdminAddr)
	if err != nil {
		return fmt.Errorf("listening on admin addr: %w", err)
	}
	s.grpcAddr.Store(grpcLn.Addr().String())
	s.httpAddr.Store(httpLn.Addr().String())
	s.adminAddr.Store(adminLn.Addr().String())

	errCh := make(chan error, 3)
	go func() { errCh <- s.grpcSrv.Serve(grpcLn) }()
	go func() { errCh <- ignoreClosed(s.httpSrv.Serve(httpLn)) }()
	go func() { errCh <- ignoreClosed(s.adminSrv.Serve(adminLn)) }()

	// Ticker.Stop does not close the channel, so a bare `for range` here
	// would leak the goroutine (and pin the whole store) on every Run exit.
	sweeper := time.NewTicker(sweepInterval)
	defer sweeper.Stop()
	sweepDone := make(chan struct{})
	defer close(sweepDone)
	go func() {
		for {
			select {
			case <-sweepDone:
				return
			case <-sweeper.C:
				s.store.Sweep()
			}
		}
	}()

	s.ready.Store(true)
	s.log.Info("conformance gateway up",
		"otlp_grpc", s.GRPCAddr(), "otlp_http", s.HTTPAddr(), "admin", s.AdminAddr())

	// Whether we exit by cancellation or because a listener failed, the
	// shutdown path below must run: sibling servers keep serving and the
	// store keeps buffering otherwise.
	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		runErr = err
	}

	s.ready.Store(false)
	s.log.Info("shutting down: draining receivers and flushing trace windows")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.grpcSrv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		s.grpcSrv.Stop()
	}
	_ = s.httpSrv.Shutdown(shutdownCtx)

	// Judge whatever is still buffered so the final report is complete.
	s.store.CloseAll()

	// Receivers are drained, so no more forward jobs will be enqueued; stop
	// the worker (drains what remains).
	if s.fwd != nil {
		s.fwd.Stop()
	}

	_ = s.adminSrv.Shutdown(shutdownCtx)
	return runErr
}

func (s *Server) adminMux(promReg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/report", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s.live.Snapshot())
	})

	mux.HandleFunc("POST /api/reset", func(w http.ResponseWriter, _ *http.Request) {
		// Drop open windows too: otherwise trace rules from pre-reset
		// telemetry re-enter the report when those windows close later.
		s.store.Reset()
		s.live.Reset()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /api/badge.svg", func(w http.ResponseWriter, r *http.Request) {
		label := "GenAI conformance"
		var score float64
		if svc := r.URL.Query().Get("service"); svc != "" {
			scores := s.live.ServiceScores() // one snapshot: check and read must agree
			got, ok := scores[svc]
			if !ok {
				http.Error(w, "unknown service", http.StatusNotFound)
				return
			}
			label, score = svc, got
		} else {
			score = s.live.Snapshot().Summary.Score
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "no-cache")
		_ = report.WriteBadgeSVG(w, label, score)
	})

	mux.Handle("GET /metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ready")
	})

	mux.Handle("GET /", webui.Handler())
	return mux
}

func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
