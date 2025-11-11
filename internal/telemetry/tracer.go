package telemetry

import (
	"context"
	"fmt"
	"log"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// InitTracer initializes OpenTelemetry tracing
func InitTracer(serviceName string) (func(), error) {
	ctx := context.Background()

	// Create OTLP exporter (to Jaeger, Tempo, etc.)
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
			semconv.DeploymentEnvironmentKey.String("production"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)), // Sample 10%
	)

	otel.SetTracerProvider(tp)

	log.Printf("✅ OpenTelemetry tracer initialized for service: %s", serviceName)

	return func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("❌ Failed to shutdown tracer: %v", err)
		}
	}, nil
}

// InitTracerWithJaeger initializes tracing with Jaeger (alternative)
func InitTracerWithJaeger(serviceName string) (func(), error) {
	ctx := context.Background()

	// For development, you can use Jaeger directly
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint("localhost:14250"), // Jaeger OTLP endpoint
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Jaeger exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // Sample all for development
	)

	otel.SetTracerProvider(tp)

	log.Printf("✅ Jaeger tracer initialized for service: %s", serviceName)

	return func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("❌ Failed to shutdown Jaeger tracer: %v", err)
		}
	}, nil
}
