package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type countingConsumer struct {
	spans   atomic.Int64
	points  atomic.Int64
	records atomic.Int64
}

func (c *countingConsumer) ConsumeTraces(td ptrace.Traces) { c.spans.Add(int64(td.SpanCount())) }
func (c *countingConsumer) ConsumeMetrics(md pmetric.Metrics) {
	c.points.Add(int64(md.DataPointCount()))
}
func (c *countingConsumer) ConsumeLogs(ld plog.Logs) { c.records.Add(int64(ld.LogRecordCount())) }

func fixtureJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/traces/valid/chat-agent.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestHTTPTracesJSON(t *testing.T) {
	c := &countingConsumer{}
	srv := httptest.NewServer(NewHTTPHandler(c))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/traces", "application/json", bytes.NewReader(fixtureJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := c.spans.Load(); got != 4 {
		t.Errorf("consumed spans = %d, want 4", got)
	}
}

func TestHTTPTracesProtobufAndGzip(t *testing.T) {
	// Build a protobuf body from the JSON fixture so both encodings share
	// one source of truth.
	req := ptraceotlp.NewExportRequest()
	if err := req.UnmarshalJSON(fixtureJSON(t)); err != nil {
		t.Fatal(err)
	}
	body, err := req.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}

	c := &countingConsumer{}
	srv := httptest.NewServer(NewHTTPHandler(c))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("protobuf status = %d, want 200", resp.StatusCode)
	}

	var gzBody bytes.Buffer
	gz := gzip.NewWriter(&gzBody)
	if _, err := gz.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil { // Close flushes; its error matters on a writer
		t.Fatal(err)
	}
	httpReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/traces", &gzBody)
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "gzip")
	resp2, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("gzip status = %d, want 200", resp2.StatusCode)
	}

	if got := c.spans.Load(); got != 8 {
		t.Errorf("consumed spans = %d, want 8 (4 per request)", got)
	}
}

func TestHTTPRejectsBadInput(t *testing.T) {
	c := &countingConsumer{}
	srv := httptest.NewServer(NewHTTPHandler(c))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/traces", "application/json", bytes.NewReader([]byte("{not json")))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: status = %d, want 400", resp.StatusCode)
	}

	resp2, err := http.Post(srv.URL+"/v1/traces", "text/plain", bytes.NewReader(fixtureJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("wrong content type: status = %d, want 415", resp2.StatusCode)
	}
}

func TestGRPCExportRoundTrip(t *testing.T) {
	c := &countingConsumer{}
	srv := NewGRPCServer(c)
	ln := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return ln.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	req := ptraceotlp.NewExportRequest()
	if err := req.UnmarshalJSON(fixtureJSON(t)); err != nil {
		t.Fatal(err)
	}
	client := ptraceotlp.NewGRPCClient(conn)
	if _, err := client.Export(context.Background(), req); err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if got := c.spans.Load(); got != 4 {
		t.Errorf("consumed spans = %d, want 4", got)
	}
}
