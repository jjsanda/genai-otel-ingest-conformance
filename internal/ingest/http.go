package ingest

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// NewHTTPHandler serves the OTLP/HTTP endpoints /v1/traces, /v1/metrics, and
// /v1/logs with protobuf and JSON payloads and optional gzip encoding —
// the same surface a collector receiver exposes.
func NewHTTPHandler(c Consumer) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", handleSignal(newTracesDecoder(c)))
	mux.HandleFunc("POST /v1/metrics", handleSignal(newMetricsDecoder(c)))
	mux.HandleFunc("POST /v1/logs", handleSignal(newLogsDecoder(c)))
	return mux
}

// signalDecoder decodes one signal's payload and dispatches to the consumer.
type signalDecoder struct {
	decode  func(body []byte, proto bool) error
	respond func(proto bool) ([]byte, error)
}

func handleSignal(d signalDecoder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var proto bool
		switch mediaType(r.Header.Get("Content-Type")) {
		case "application/x-protobuf":
			proto = true
		case "application/json":
			proto = false
		default:
			http.Error(w, "unsupported content type (want application/x-protobuf or application/json)", http.StatusUnsupportedMediaType)
			return
		}

		body, err := readBody(r)
		if errors.Is(err, errBodyTooLarge) {
			http.Error(w, fmt.Sprintf("payload exceeds %d bytes", maxRecvBytes), http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("reading body: %v", err), http.StatusBadRequest)
			return
		}
		if err := d.decode(body, proto); err != nil {
			http.Error(w, fmt.Sprintf("decoding OTLP payload: %v", err), http.StatusBadRequest)
			return
		}

		resp, err := d.respond(proto)
		if err != nil {
			http.Error(w, "encoding response", http.StatusInternalServerError)
			return
		}
		if proto {
			w.Header().Set("Content-Type", "application/x-protobuf")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp)
	}
}

var errBodyTooLarge = errors.New("body exceeds the receiver limit")

func readBody(r *http.Request) ([]byte, error) {
	reader := io.Reader(r.Body)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		// Decompression errors surface through Read; the reader's Close
		// result carries nothing actionable here.
		defer func() { _ = gz.Close() }()
		reader = gz
	}
	// Read one byte past the limit: a plain LimitReader would silently
	// truncate an oversize batch, and a protobuf cut on a field boundary
	// still unmarshals — the sender would get 200 OK for dropped spans.
	body, err := io.ReadAll(io.LimitReader(reader, maxRecvBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRecvBytes {
		return nil, errBodyTooLarge
	}
	return body, nil
}

func mediaType(contentType string) string {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	return strings.TrimSpace(strings.ToLower(contentType))
}
