package observability

import (
	"context"
	"fmt"

	"github.com/grs/klaviger/internal/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// SetupTracing initializes OpenTelemetry tracing
func SetupTracing(ctx context.Context, cfg *config.TracingConfig) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	// Create OTLP HTTP exporter
	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithInsecure(), // TODO: Make configurable
	)
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource with service name
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	// Register as global tracer provider
	otel.SetTracerProvider(tp)

	// Return shutdown function
	return tp.Shutdown, nil
}
