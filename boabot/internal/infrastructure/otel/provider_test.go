package otel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gotel "go.opentelemetry.io/otel"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	infraotel "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/otel"
)

func TestNew_NoEndpoint_ReturnsNoOp(t *testing.T) {
	t.Parallel()

	cfg := infraotel.OTelConfig{
		ServiceName:    "test-svc",
		ServiceVersion: "0.0.1",
		Environment:    "test",
		Endpoint:       "", // no-op path
	}

	p, err := infraotel.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil Provider")
	}
	if p.TracerProvider == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	if p.MeterProvider == nil {
		t.Fatal("expected non-nil MeterProvider")
	}
	if p.Shutdown == nil {
		t.Fatal("expected non-nil Shutdown func")
	}

	// Shutdown on no-op must not error.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
}

func TestNew_WithEndpoint_Initialises(t *testing.T) {
	t.Parallel()

	// Minimal OTLP-accepting server: accept POST on any path, return 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := infraotel.OTelConfig{
		ServiceName:    "test-svc",
		ServiceVersion: "0.1.0",
		Environment:    "ci",
		Endpoint:       srv.Listener.Addr().String(),
		Insecure:       true,
	}

	p, err := infraotel.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil Provider")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	t.Parallel()

	cfg := infraotel.OTelConfig{
		ServiceName: "test-svc",
		Endpoint:    "",
	}

	p, err := infraotel.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Call Shutdown twice — both should succeed without error.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown returned error: %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown returned error: %v", err)
	}
}

func TestNew_SetsGlobalProviders(t *testing.T) {
	// Not parallel: mutates global OTel state.
	cfg := infraotel.OTelConfig{
		ServiceName:    "global-test-svc",
		ServiceVersion: "1.0.0",
		Environment:    "test",
		Endpoint:       "", // no-op but still sets globals
	}

	p, err := infraotel.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	// When Endpoint is empty we set noop providers as globals.
	// Verify the global tracer provider is the noop SDK type.
	tp := gotel.GetTracerProvider()
	if _, isNoop := tp.(tracenoop.TracerProvider); !isNoop {
		t.Errorf("expected global TracerProvider to be noop.TracerProvider, got %T", tp)
	}
}
