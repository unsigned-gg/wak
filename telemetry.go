// telemetry.go — OTLP tracing initialization + HTTP traceparent propagation.
//
// Waker exports traces to alloy-gateway (OTLP gRPC, :4317) in the monitoring
// namespace. The endpoint is env-configurable (OTEL_EXPORTER_OTLP_ENDPOINT),
// defaulting to the in-cluster alloy-gateway Service. Tracing can be disabled
// by setting OTEL_TRACES_ENABLED=false (useful for local dev).
//
// The health server middleware extracts inbound traceparent headers so that
// probes from the soma-operator or monitoring stack propagate context rather
// than creating orphan traces.

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultOTLPEndpoint = "alloy-gateway.monitoring.svc:4317"
	otelEnabledEnv      = "OTEL_TRACES_ENABLED"
	otelEndpointEnv     = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

// initTracer configures the global TracerProvider with an OTLP gRPC exporter
// pointing at the alloy-gateway. Returns a shutdown function.
func initTracer(ctx context.Context, log *slog.Logger) func(context.Context) error {
	if os.Getenv(otelEnabledEnv) == "false" {
		log.Info("tracing disabled", "reason", otelEnabledEnv+"=false")
		return func(context.Context) error { return nil }
	}

	endpoint := os.Getenv(otelEndpointEnv)
	if endpoint == "" {
		endpoint = defaultOTLPEndpoint
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // in-cluster, no TLS
	)
	if err != nil {
		log.Error("failed to create OTLP exporter", "endpoint", endpoint, "err", err)
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("waker"),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		log.Error("failed to create trace resource", "err", err)
		return func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	log.Info("tracing initialized", "endpoint", endpoint, "service", "waker")
	return tp.Shutdown
}

// tracingMiddleware extracts inbound traceparent headers from the HTTP request
// and creates a child span around the handler. This prevents orphan traces when
// the soma-operator or monitoring stack probes waker's health endpoints.
func tracingMiddleware(name string, next http.HandlerFunc) http.HandlerFunc {
	tracer := otel.Tracer("waker/health")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, name)
		defer span.End()
		next(w, r.WithContext(ctx))
	}
}

// startSpan is a convenience wrapper that starts a span from the waker tracer.
func startSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("waker").Start(ctx, name)
}

// fmtDuration formats a duration for trace attributes (readable, not raw ns).
func fmtDuration(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}
