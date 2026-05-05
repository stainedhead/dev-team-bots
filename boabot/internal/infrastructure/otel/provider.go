// Package otel initialises OpenTelemetry trace and metric providers for the
// boabot runtime. When no endpoint is configured, no-op providers are returned
// so the application starts without any observability backend.
package otel

import (
	"context"
	"errors"
	"fmt"

	gotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otlpmetrichttp "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otlptracehttp "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// OTelConfig holds the configuration for the OTel provider.
type OTelConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Endpoint       string // OTLP/HTTP endpoint; empty string = no-op providers
	Insecure       bool
}

// Provider holds the initialised tracer and meter providers and a Shutdown func
// that flushes and closes both.
type Provider struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Shutdown       func(ctx context.Context) error
}

// New constructs a Provider. If cfg.Endpoint is empty, no-op providers are
// returned and the global OTel providers are set to no-op. Otherwise, OTLP/HTTP
// exporters are created targeting cfg.Endpoint.
func New(ctx context.Context, cfg OTelConfig) (*Provider, error) {
	if cfg.Endpoint == "" {
		tp := tracenoop.NewTracerProvider()
		mp := metricnoop.NewMeterProvider()
		gotel.SetTracerProvider(tp)
		return &Provider{
			TracerProvider: tp,
			MeterProvider:  mp,
			Shutdown:       func(context.Context) error { return nil },
		}, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}

	// Trace exporter.
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}
	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("otel: create trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	// Metric exporter.
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("otel: create metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)

	gotel.SetTracerProvider(tp)

	shutdown := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}

	return &Provider{
		TracerProvider: tp,
		MeterProvider:  mp,
		Shutdown:       shutdown,
	}, nil
}
