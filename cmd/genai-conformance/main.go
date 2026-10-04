// Command genai-conformance validates GenAI telemetry against the
// OpenTelemetry GenAI semantic conventions.
package main

import (
	"os"

	"github.com/jjsanda/genai-otel-ingest-conformance/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
