package options

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
)

var _ IOptions = (*JaegerOptions)(nil)

// defaultJaegerServer is a bare host:port, which is what
// otlptracegrpc.WithEndpoint requires. 4317 is the OTLP/gRPC port of an
// OpenTelemetry collector (Jaeger's OTLP receiver). The previous default,
// "http://127.0.0.1:14268/api/traces", is Jaeger's legacy thrift HTTP endpoint:
// it is a URL (rejected by WithEndpoint) on a port that does not speak OTLP
// gRPC, so the exporter target was invalid and tracing failed silently.
const defaultJaegerServer = "127.0.0.1:4317"

// JaegerOptions defines options for consul client.
type JaegerOptions struct {
	// Server is the url of the Jaeger server. It accepts either a bare
	// "host:port" (OTLP gRPC, the recommended form) or a full http(s) URL.
	Server      string `json:"server,omitempty" mapstructure:"server"`
	ServiceName string `json:"service-name,omitempty" mapstructure:"service-name"`
	Env         string `json:"env,omitempty" mapstructure:"env"`

	// tp is the tracer provider installed by SetTracerProvider. It is retained
	// so that Shutdown can flush the batched spans on process exit.
	tp *tracesdk.TracerProvider
}

// NewJaegerOptions create a `zero` value instance.
func NewJaegerOptions() *JaegerOptions {
	return &JaegerOptions{
		Server: defaultJaegerServer,
		Env:    "dev",
	}
}

// parseJaegerEndpoint classifies the configured endpoint.
//
// It returns the endpoint to hand to the OTLP exporter plus whether it is a full
// URL. otlptracegrpc.WithEndpoint only accepts a bare "host:port"; a URL has to
// be passed to otlptracegrpc.WithEndpointURL (which also derives the
// insecure/TLS setting from the scheme) instead.
func parseJaegerEndpoint(server string) (string, bool, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", false, fmt.Errorf("--jaeger.server can not be empty")
	}

	if strings.Contains(server, "://") {
		u, err := url.Parse(server)
		if err != nil {
			return "", false, fmt.Errorf("invalid jaeger server url %q: %w", server, err)
		}

		if u.Scheme != "http" && u.Scheme != "https" {
			return "", false, fmt.Errorf("invalid jaeger server url %q: unsupported scheme %q", server, u.Scheme)
		}

		if u.Host == "" {
			return "", false, fmt.Errorf("invalid jaeger server url %q: missing host", server)
		}

		return server, true, nil
	}

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		return "", false, fmt.Errorf("invalid jaeger server %q, expected host:port: %w", server, err)
	}

	if host == "" {
		return "", false, fmt.Errorf("invalid jaeger server %q: missing host", server)
	}

	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", false, fmt.Errorf("invalid jaeger server %q: port must be a number: %w", server, err)
	}

	return server, false, nil
}

// Validate verifies flags passed to JaegerOptions.
func (o *JaegerOptions) Validate() []error {
	errs := []error{}

	if _, _, err := parseJaegerEndpoint(o.Server); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// AddFlags adds flags related to jaeger tracing for a specific APIServer to the specified FlagSet.
func (o *JaegerOptions) AddFlags(fs *pflag.FlagSet, prefixes ...string) {
	fs.StringVar(&o.Server, join(prefixes...)+"jaeger.server", o.Server, ""+
		"Server is the url of the Jaeger server.")
	fs.StringVar(&o.ServiceName, join(prefixes...)+"jaeger.service-name", o.ServiceName, ""+
		"Specify the service name for jaeger resource.")
	fs.StringVar(&o.Env, join(prefixes...)+"jaeger.env", o.Env, "Specify the deployment environment(dev/test/staging/prod).")
}

// newTracerProvider builds a tracer provider that exports spans through the given
// exporter.
func newTracerProvider(exporter tracesdk.SpanExporter, serviceName, env string) (*tracesdk.TracerProvider, error) {
	res, err := resource.New(context.Background(), resource.WithAttributes(
		semconv.ServiceNameKey.String(serviceName),
		attribute.String("env", env),
		attribute.String("exporter", "jaeger"),
	))
	if err != nil {
		return nil, err
	}

	// batch span processor to aggregate spans before export.
	bsp := tracesdk.NewBatchSpanProcessor(exporter)

	return tracesdk.NewTracerProvider(
		// Set the sampling rate based on the parent span to 100%
		tracesdk.WithSampler(tracesdk.ParentBased(tracesdk.TraceIDRatioBased(1.0))),
		// Always be sure to batch in production.
		tracesdk.WithSpanProcessor(bsp),
		// Record information about this application in an Resource.
		tracesdk.WithResource(res),
	), nil
}

// SetTracerProvider creates an OTLP exporter, installs a tracer provider
// globally and remembers it for a later Shutdown call.
func (o *JaegerOptions) SetTracerProvider() error {
	endpoint, isURL, err := parseJaegerEndpoint(o.Server)
	if err != nil {
		return err
	}

	opts := make([]otlptracegrpc.Option, 0, 2)
	if isURL {
		// Full URL: WithEndpointURL derives the insecure/TLS setting from the
		// scheme, so WithInsecure must not be combined with it.
		opts = append(opts, otlptracegrpc.WithEndpointURL(endpoint))
	} else {
		// Bare host:port with plain gRPC.
		opts = append(opts, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(context.Background(), opts...)
	if err != nil {
		return err
	}

	return o.setTracerProvider(exporter)
}

// setTracerProvider wires the given exporter into a tracer provider and installs
// it as the global provider.
func (o *JaegerOptions) setTracerProvider(exporter tracesdk.SpanExporter) error {
	tp, err := newTracerProvider(exporter, o.ServiceName, o.Env)
	if err != nil {
		return err
	}

	otel.SetTracerProvider(tp)

	o.tp = tp

	return nil
}

// TracerProvider returns the tracer provider installed by SetTracerProvider, or
// nil if none was installed.
func (o *JaegerOptions) TracerProvider() *tracesdk.TracerProvider {
	return o.tp
}

// Shutdown flushes and stops the tracer provider installed by
// SetTracerProvider. Without it the batched spans are lost when the process
// exits. It is a no-op when SetTracerProvider was never called.
func (o *JaegerOptions) Shutdown(ctx context.Context) error {
	if o.tp == nil {
		return nil
	}

	err := o.tp.Shutdown(ctx)
	o.tp = nil

	return err
}
