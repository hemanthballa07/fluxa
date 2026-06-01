// Package observability wires OpenTelemetry tracing for the fluxa Go services.
// Init is fail-open: any setup error logs and installs a no-op shutdown so that
// tracing never blocks or fails the pipeline (same spirit as the ML scorer's
// fail-open behavior).
package observability

import (
	"context"
	"os"
	"strings"

	"github.com/fluxa/fluxa/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// noopShutdown is returned whenever tracing is disabled or failed to init.
func noopShutdown(context.Context) error { return nil }

// endpoint resolves the OTLP collector address as host:port, stripping any
// URL scheme. Defaults to the compose service name.
func endpoint() string {
	ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if ep == "" {
		return "jaeger:4317"
	}
	ep = strings.TrimPrefix(ep, "http://")
	ep = strings.TrimPrefix(ep, "https://")
	return ep
}

// Init installs a global TracerProvider exporting over OTLP/gRPC (insecure,
// local) plus a W3C trace-context propagator, and returns a shutdown func that
// flushes pending spans. On any failure it logs and returns a no-op shutdown;
// it never returns an error and never blocks startup.
func Init(serviceName string) func(context.Context) error {
	log := logging.NewLogger(serviceName, "tracing")

	exp, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithEndpoint(endpoint()),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Warn("tracing exporter init failed; running without traces", map[string]interface{}{"error": err.Error()})
		return noopShutdown
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
		// AlwaysSample for local demos; prod can override via OTEL_TRACES_SAMPLER.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	log.Info("tracing initialized", map[string]interface{}{"endpoint": endpoint()})
	return tp.Shutdown
}
