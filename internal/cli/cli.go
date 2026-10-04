// Package cli implements the genai-conformance command-line interface.
//
// The binary has three subcommands sharing one conformance engine:
//
//	serve     run the OTLP ingest gateway with live conformance checking
//	validate  validate recorded OTLP data offline (CI gate)
//	rules     inspect the conformance rule catalog
package cli

import (
	"fmt"
	"io"
)

// Exit codes returned by Main. They are part of the CI-gate contract:
// validate exits 0 when telemetry is conformant, 1 when blocking findings
// exist, and 2 on usage or input errors.
const (
	ExitOK       = 0
	ExitFindings = 1
	ExitUsage    = 2
)

// Version is the CLI version, overridable at build time via
// -ldflags "-X github.com/jjsanda/genai-otel-ingest-conformance/internal/cli.Version=v1.2.3".
var Version = "0.1.0-dev"

const usage = `genai-conformance validates GenAI telemetry against the
OpenTelemetry GenAI semantic conventions.

Usage:
  genai-conformance <command> [flags]

Commands:
  serve      Run the OTLP ingest gateway with live conformance checking.
  validate   Validate recorded OTLP data (binpb, OTLP/JSON, JSONL) offline.
  rules      Inspect the conformance rule catalog.
  version    Print version information.

Run "genai-conformance <command> -h" for command-specific flags.
`

// Main dispatches to the requested subcommand and returns the process exit
// code. It never calls os.Exit itself so tests can drive it directly.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "rules":
		return runRules(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "genai-conformance", Version)
		return ExitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "genai-conformance: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}
