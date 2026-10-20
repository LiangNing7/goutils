package options

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

type fakeSpanExporter struct {
	mu    sync.Mutex
	spans []tracesdk.ReadOnlySpan
}

func (f *fakeSpanExporter) ExportSpans(_ context.Context, spans []tracesdk.ReadOnlySpan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spans = append(f.spans, spans...)

	return nil
}

func (f *fakeSpanExporter) Shutdown(_ context.Context) error { return nil }

func (f *fakeSpanExporter) SpanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.spans)
}

// TestJaegerOptions_DefaultEndpointIsHostPort pins the default OTLP/gRPC
// endpoint: it must be a bare host:port, because otlptracegrpc.WithEndpoint
// rejects URLs and 14268 is the Jaeger thrift HTTP port, not OTLP gRPC (4317).
func TestJaegerOptions_DefaultEndpointIsHostPort(t *testing.T) {
	o := NewJaegerOptions()

	require.Equal(t, "127.0.0.1:4317", o.Server)

	host, port, err := net.SplitHostPort(o.Server)
	require.NoError(t, err, "default endpoint must be a valid host:port")
	require.Equal(t, "127.0.0.1", host)
	require.Equal(t, "4317", port)

	require.Empty(t, o.Validate())
}

func TestJaegerOptions_ParseEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		server   string
		want     string
		wantURL  bool
		wantFail bool
	}{
		{name: "host:port", server: "127.0.0.1:4317", want: "127.0.0.1:4317"},
		{name: "hostname:port", server: "otel-collector:4317", want: "otel-collector:4317"},
		{name: "http url", server: "http://collector:4318/v1/traces", want: "http://collector:4318/v1/traces", wantURL: true},
		{name: "https url", server: "https://collector:4318/v1/traces", want: "https://collector:4318/v1/traces", wantURL: true},
		{name: "empty", server: "", wantFail: true},
		{name: "plain text", server: "not a url at all", wantFail: true},
		{name: "missing port", server: "127.0.0.1", wantFail: true},
		{name: "empty host", server: ":4317", wantFail: true},
		{name: "non numeric port", server: "127.0.0.1:abc", wantFail: true},
		{name: "scheme only", server: "http://", wantFail: true},
		{name: "unsupported scheme", server: "mongodb://host:27017", wantFail: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			endpoint, isURL, err := parseJaegerEndpoint(c.server)
			if c.wantFail {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.Equal(t, c.want, endpoint)
			require.Equal(t, c.wantURL, isURL)
		})
	}
}

func TestJaegerOptions_Validate(t *testing.T) {
	o := NewJaegerOptions()

	o.Server = "http://collector:4318/v1/traces"
	require.Empty(t, o.Validate())

	o.Server = ""
	require.NotEmpty(t, o.Validate())

	o.Server = "not a url at all"
	require.NotEmpty(t, o.Validate())

	o.Server = "127.0.0.1:4317"
	require.Empty(t, o.Validate())
}

// TestJaegerOptions_ExportSpans proves spans actually reach the exporter that
// SetTracerProvider wires into the batch span processor.
func TestJaegerOptions_ExportSpans(t *testing.T) {
	exp := &fakeSpanExporter{}

	tp, err := newTracerProvider(exp, "svc", "dev")
	require.NoError(t, err)
	require.NotNil(t, tp)

	_, span := tp.Tracer("test").Start(context.Background(), "op")
	span.End()

	require.NoError(t, tp.Shutdown(context.Background()))
	require.Equal(t, 1, exp.SpanCount(), "the exported span must reach the exporter")
}

// TestJaegerOptions_Shutdown verifies that the tracer provider is retained so
// that buffered spans can be flushed on process exit.
func TestJaegerOptions_Shutdown(t *testing.T) {
	o := NewJaegerOptions()
	o.ServiceName = "svc"

	// Shutdown before SetTracerProvider must be a safe no-op.
	require.NoError(t, o.Shutdown(context.Background()))
	require.Nil(t, o.TracerProvider())

	require.NoError(t, o.setTracerProvider(&fakeSpanExporter{}))
	require.NotNil(t, o.TracerProvider())

	require.NoError(t, o.Shutdown(context.Background()))
	require.Nil(t, o.TracerProvider(), "Shutdown must release the provider")
}

// TestJaegerOptions_SetTracerProvider_InvalidEndpoint proves SetTracerProvider
// refuses to build an exporter for a bogus endpoint instead of failing silently
// at export time.
func TestJaegerOptions_SetTracerProvider_InvalidEndpoint(t *testing.T) {
	o := NewJaegerOptions()
	o.Server = "not a url at all"

	require.Error(t, o.SetTracerProvider())
	require.Nil(t, o.TracerProvider())
}

// TestJaegerOptions_SetTracerProvider_ExportsToOTLPGRPC is an end-to-end check
// that the generated endpoint really is a usable OTLP/gRPC target: it starts a
// local OTLP gRPC collector (no real Jaeger/collector involved) and asserts the
// span arrives.
func TestJaegerOptions_SetTracerProvider_ExportsToOTLPGRPC(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	collector := &fakeOTLPCollector{}
	grpcServer := grpc.NewServer()
	coltracepb.RegisterTraceServiceServer(grpcServer, collector)

	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	o := NewJaegerOptions()
	o.Server = lis.Addr().String() // bare host:port
	o.ServiceName = "goutils-test"

	require.Empty(t, o.Validate())
	require.NoError(t, o.SetTracerProvider())

	_, span := otel.Tracer("goutils").Start(context.Background(), "op")
	span.End()

	// Shutdown flushes the batch span processor, so the export completes before
	// it returns.
	require.NoError(t, o.Shutdown(context.Background()))
	require.NotEmpty(t, collector.ResourceSpans(), "the span must reach the OTLP gRPC collector")
}

// fakeOTLPCollector implements the OTLP trace service and records what it gets.
type fakeOTLPCollector struct {
	coltracepb.UnimplementedTraceServiceServer

	mu     sync.Mutex
	spans  []*tracepb.ResourceSpans
	export error
}

func (c *fakeOTLPCollector) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.export != nil {
		return nil, c.export
	}

	c.spans = append(c.spans, req.GetResourceSpans()...)

	return &coltracepb.ExportTraceServiceResponse{}, nil
}

func (c *fakeOTLPCollector) ResourceSpans() []*tracepb.ResourceSpans {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.spans
}
