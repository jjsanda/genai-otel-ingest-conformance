// Package otlpio decodes recorded OTLP telemetry files for offline
// validation. Three encodings are supported:
//
//   - *.traces.binpb, *.metrics.binpb, *.logs.binpb — an OTLP protobuf
//     ExportRequest. Protobuf is the primary cross-language interchange
//     format because protobuf's canonical JSON encodes trace/span IDs as
//     base64 while the OTLP/JSON spec requires hex; binary bytes have no
//     such ambiguity (see ADR-0005).
//   - *.json — one OTLP/JSON object (spec encoding: hex IDs, camelCase);
//     the signal is detected by decoding.
//   - *.jsonl — one OTLP/JSON object per line, as written by the collector
//     file exporter.
package otlpio

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

// Data bundles decoded telemetry batches from input files.
type Data struct {
	Traces  []ptrace.Traces
	Metrics []pmetric.Metrics
	Logs    []plog.Logs
}

// Empty reports whether nothing was decoded.
func (d *Data) Empty() bool {
	return len(d.Traces) == 0 && len(d.Metrics) == 0 && len(d.Logs) == 0
}

// ReadFile decodes one telemetry file, detecting the encoding from the file
// name and the signal from the content (or the extension for protobuf).
func ReadFile(path string) (*Data, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasSuffix(path, ".binpb") || strings.HasSuffix(path, ".pb"):
		return readProto(path, raw)
	case strings.HasSuffix(path, ".jsonl"):
		return readJSONLines(path, raw)
	case strings.HasSuffix(path, ".json"):
		d := &Data{}
		if err := decodeJSONObject(raw, d); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if d.Empty() {
			return nil, fmt.Errorf("%s: no telemetry found (empty OTLP/JSON object?)", path)
		}
		return d, nil
	default:
		return nil, fmt.Errorf("%s: unsupported file type (expected .traces.binpb/.metrics.binpb/.logs.binpb, .json, or .jsonl)", path)
	}
}

// readProto decodes an OTLP protobuf ExportRequest; the signal must be named
// in the extension because the three request messages are not distinguishable
// by content alone.
func readProto(path string, raw []byte) (*Data, error) {
	d := &Data{}
	switch {
	case strings.HasSuffix(path, ".traces.binpb") || strings.HasSuffix(path, ".traces.pb"):
		req := ptraceotlp.NewExportRequest()
		if err := req.UnmarshalProto(raw); err != nil {
			return nil, fmt.Errorf("%s: decoding OTLP traces protobuf: %w", path, err)
		}
		d.Traces = append(d.Traces, req.Traces())
	case strings.HasSuffix(path, ".metrics.binpb") || strings.HasSuffix(path, ".metrics.pb"):
		req := pmetricotlp.NewExportRequest()
		if err := req.UnmarshalProto(raw); err != nil {
			return nil, fmt.Errorf("%s: decoding OTLP metrics protobuf: %w", path, err)
		}
		d.Metrics = append(d.Metrics, req.Metrics())
	case strings.HasSuffix(path, ".logs.binpb") || strings.HasSuffix(path, ".logs.pb"):
		req := plogotlp.NewExportRequest()
		if err := req.UnmarshalProto(raw); err != nil {
			return nil, fmt.Errorf("%s: decoding OTLP logs protobuf: %w", path, err)
		}
		d.Logs = append(d.Logs, req.Logs())
	default:
		return nil, fmt.Errorf("%s: protobuf inputs must name their signal: *.traces.binpb, *.metrics.binpb, or *.logs.binpb", path)
	}
	return d, nil
}

// decodeJSONObject tries the three OTLP/JSON request shapes; the pdata
// unmarshalers skip unknown fields, so "decoded but empty" means the object
// belongs to another signal.
func decodeJSONObject(raw []byte, into *Data) error {
	tr := ptraceotlp.NewExportRequest()
	if err := tr.UnmarshalJSON(raw); err == nil && tr.Traces().SpanCount() > 0 {
		into.Traces = append(into.Traces, tr.Traces())
		return nil
	}
	mr := pmetricotlp.NewExportRequest()
	if err := mr.UnmarshalJSON(raw); err == nil && mr.Metrics().DataPointCount() > 0 {
		into.Metrics = append(into.Metrics, mr.Metrics())
		return nil
	}
	lr := plogotlp.NewExportRequest()
	if err := lr.UnmarshalJSON(raw); err == nil && lr.Logs().LogRecordCount() > 0 {
		into.Logs = append(into.Logs, lr.Logs())
		return nil
	}
	// Re-run the traces decode to surface a real error message.
	if err := ptraceotlp.NewExportRequest().UnmarshalJSON(raw); err != nil {
		return fmt.Errorf("not valid OTLP/JSON: %w", err)
	}
	return fmt.Errorf("valid JSON but contains no spans, data points, or log records")
}

func readJSONLines(path string, raw []byte) (*Data, error) {
	d := &Data{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := bytes.TrimSpace(sc.Bytes())
		if len(text) == 0 {
			continue
		}
		if err := decodeJSONObject(text, d); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if d.Empty() {
		return nil, fmt.Errorf("%s: no telemetry found", path)
	}
	return d, nil
}
