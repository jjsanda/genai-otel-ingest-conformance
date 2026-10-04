package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/assembly"
)

// TestGatewaySmoke boots the full gateway on ephemeral ports, sends the
// exemplary fixture over OTLP/HTTP, and verifies the report, badge, metrics,
// and health endpoints — then shuts down gracefully.
func TestGatewaySmoke(t *testing.T) {
	srv, err := New(Config{
		GRPCAddr:  "127.0.0.1:0",
		HTTPAddr:  "127.0.0.1:0",
		AdminAddr: "127.0.0.1:0",
		Window:    assembly.Config{IdleTimeout: 100 * time.Millisecond, MaxAge: time.Second},
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()

	waitFor(t, "server ready", func() bool {
		if srv.AdminAddr() == "" {
			return false
		}
		resp, err := http.Get("http://" + srv.AdminAddr() + "/readyz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	fixture, err := os.ReadFile("../../testdata/fixtures/traces/valid/chat-agent.json")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post("http://"+srv.HTTPAddr()+"/v1/traces", "application/json", bytes.NewReader(fixture))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest status = %d, want 200", resp.StatusCode)
	}

	var report struct {
		Summary struct {
			Score  float64 `json:"score"`
			Checks int     `json:"checks"`
		} `json:"summary"`
		Services map[string]struct {
			Score float64 `json:"score"`
		} `json:"services"`
		Rules []struct {
			RuleID string `json:"rule_id"`
		} `json:"rules"`
	}
	waitFor(t, "report populated", func() bool {
		raw := getBody(t, "http://"+srv.AdminAddr()+"/api/report")
		if err := json.Unmarshal(raw, &report); err != nil {
			return false
		}
		return report.Summary.Checks > 0
	})

	if _, ok := report.Services["demo-travel-agent"]; !ok {
		t.Errorf("services missing demo-travel-agent: %+v", report.Services)
	}
	if report.Summary.Score != 1 {
		t.Errorf("score = %v, want 1.0 for the exemplary fixture", report.Summary.Score)
	}
	if len(report.Rules) < 18 {
		t.Errorf("rule catalog in report has %d entries, want the full catalog", len(report.Rules))
	}

	metrics := string(getBody(t, "http://"+srv.AdminAddr()+"/metrics"))
	for _, want := range []string{
		"genai_conformance_spans_received_total 4",
		`genai_conformance_score{service="demo-travel-agent"} 1`,
		"genai_conformance_traces_open",
		"genai_conformance_build_info",
	} {
		if !strings.Contains(metrics, want) {
			t.Errorf("/metrics missing %q", want)
		}
	}

	badge := string(getBody(t, "http://"+srv.AdminAddr()+"/api/badge.svg"))
	if !strings.Contains(badge, "100.0%") || !strings.Contains(badge, "<svg") {
		t.Errorf("badge does not show a perfect score: %s", badge)
	}

	page := string(getBody(t, "http://"+srv.AdminAddr()+"/"))
	if !strings.Contains(page, "GenAI telemetry conformance") {
		t.Error("report UI not served at /")
	}

	// Reset clears the aggregate.
	req, _ := http.NewRequest(http.MethodPost, "http://"+srv.AdminAddr()+"/api/reset", nil)
	rr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = rr.Body.Close()
	raw := getBody(t, "http://"+srv.AdminAddr()+"/api/report")
	var after struct {
		Summary struct {
			Checks int `json:"checks"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if after.Summary.Checks != 0 {
		t.Errorf("checks after reset = %d, want 0", after.Summary.Checks)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("graceful shutdown timed out")
	}
}

func getBody(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", url, resp.StatusCode, raw)
	}
	return raw
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestBadgeUnknownService covers the 404 path.
func TestBadgeUnknownService(t *testing.T) {
	srv, err := New(Config{
		GRPCAddr:  "127.0.0.1:0",
		HTTPAddr:  "127.0.0.1:0",
		AdminAddr: "127.0.0.1:0",
		Version:   "test",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitFor(t, "ready", func() bool {
		if srv.AdminAddr() == "" {
			return false
		}
		resp, err := http.Get(fmt.Sprintf("http://%s/readyz", srv.AdminAddr()))
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	resp, err := http.Get("http://" + srv.AdminAddr() + "/api/badge.svg?service=nope")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown service badge: status = %d, want 404", resp.StatusCode)
	}
	cancel()
	<-done
}
