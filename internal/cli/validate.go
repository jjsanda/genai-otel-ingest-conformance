package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/engine"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/otlpio"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/registry"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/report"
	"github.com/jjsanda/genai-otel-ingest-conformance/internal/rules"
)

const validateUsage = `Validate recorded OTLP telemetry against the GenAI semantic conventions.

Usage:
  genai-conformance validate [flags] <file>...

Accepted inputs:
  *.traces.binpb / *.metrics.binpb / *.logs.binpb   OTLP protobuf ExportRequest
  *.json                                            OTLP/JSON (spec encoding)
  *.jsonl                                           collector file-exporter lines

Exit codes: 0 conformant, 1 blocking findings, 2 usage or input error.

Flags:
`

// runValidate is the offline CI gate: read files, evaluate, render, exit.
func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "markdown", `output format: "markdown", "json", or "junit"`)
	strict := fs.Bool("strict", false, "exit 1 on any finding, not only ERROR findings")
	outPath := fs.String("o", "", "write the report to this file instead of stdout")
	fs.Usage = func() {
		fmt.Fprint(stderr, validateUsage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	files := fs.Args()
	if len(files) == 0 {
		fs.Usage()
		return ExitUsage
	}

	reg := registry.Default()
	eng := engine.New(reg, rules.All(reg)...)

	total := engine.NewResult()
	var traceBatches []ptrace.Traces
	for _, path := range files {
		data, err := otlpio.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "genai-conformance validate: %v\n", err)
			return ExitUsage
		}
		for _, td := range data.Traces {
			total.Merge(eng.EvaluateTraces(td))
			traceBatches = append(traceBatches, td)
		}
		for _, md := range data.Metrics {
			total.Merge(eng.EvaluateMetrics(md))
		}
		for _, ld := range data.Logs {
			total.Merge(eng.EvaluateLogs(ld))
		}
	}
	// Offline inputs are the complete universe of spans, so traces can be
	// assembled exactly — no windowing heuristics needed here.
	for _, tc := range eng.AssembleTraces(traceBatches) {
		total.Merge(eng.EvaluateTrace(tc))
	}

	rep := report.Build(total, reg, Version)

	var buf bytes.Buffer
	var err error
	switch *format {
	case "json":
		err = report.WriteJSON(&buf, rep)
	case "markdown":
		err = report.WriteMarkdown(&buf, rep)
	case "junit":
		err = report.WriteJUnit(&buf, rep)
	default:
		fmt.Fprintf(stderr, "genai-conformance validate: unknown format %q (want markdown, json, or junit)\n", *format)
		return ExitUsage
	}
	if err != nil {
		fmt.Fprintf(stderr, "genai-conformance validate: rendering report: %v\n", err)
		return ExitUsage
	}

	if *outPath != "" {
		if err := os.WriteFile(*outPath, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintf(stderr, "genai-conformance validate: %v\n", err)
			return ExitUsage
		}
		fmt.Fprintf(stderr, "report written to %s\n", *outPath)
	} else {
		if _, err := stdout.Write(buf.Bytes()); err != nil {
			return ExitUsage
		}
	}

	if rep.Summary.Errors > 0 || (*strict && len(rep.Findings) > 0) {
		return ExitFindings
	}
	return ExitOK
}
