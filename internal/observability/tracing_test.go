package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestInitReturnsUsableTracerAndSafeShutdown(t *testing.T) {
	shutdown := Init("test-svc")
	if shutdown == nil {
		t.Fatal("Init returned a nil shutdown func")
	}

	// After Init a real (sampling) provider must be installed: a started span
	// records and carries a valid trace ID. This proves the provider isn't the
	// default no-op.
	ctx, span := otel.Tracer("test").Start(context.Background(), "unit-span")
	if !span.IsRecording() {
		t.Error("expected a recording span after Init; got a no-op span")
	}
	if !span.SpanContext().TraceID().IsValid() {
		t.Error("expected a valid TraceID after Init")
	}

	// The W3C propagator must be installed: injecting the active span context
	// must emit a `traceparent` header. The default propagator is non-nil but a
	// no-op, so a nil-check would be a tautology — inject behavior is the real
	// proof that Init set propagation.TraceContext.
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if _, ok := carrier["traceparent"]; !ok {
		t.Error("expected a traceparent after Init; W3C propagator not installed")
	}
	span.End()

	// Shutdown must be safe to call and must not error on a clean provider.
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}
