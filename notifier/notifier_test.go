package notifier

import (
	"context"
	"sync"
	"testing"

	"github.com/go-coldbrew/errors"
	"github.com/go-coldbrew/options"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/metadata"
)

func TestGetTraceId_NonStringValue(t *testing.T) {
	// Regression test: GetTraceId must not panic when the tracerID
	// option holds a non-string value.
	ctx := options.AddToOptions(context.Background(), tracerID, 12345)

	// Before the fix this panicked with "interface conversion: interface {} is int, not string".
	got := GetTraceId(ctx)
	if got != "" {
		t.Errorf("expected empty string for non-string tracerID, got %q", got)
	}
}

func TestGetTraceId_StringValue(t *testing.T) {
	ctx := options.AddToOptions(context.Background(), tracerID, "abc-123")

	got := GetTraceId(ctx)
	if got != "abc-123" {
		t.Errorf("expected 'abc-123', got %q", got)
	}
}

func TestNotifyAsync_BoundedConcurrency(t *testing.T) {
	// Use a 1-slot semaphore and pre-fill it to simulate a full pool.
	ch := make(chan struct{}, 1)
	ch <- struct{}{} // pre-fill: pool is now full
	asyncSem.Store(&ch)
	t.Cleanup(func() {
		// Restore default. Drain first so cleanup is safe.
		for len(ch) > 0 {
			<-ch
		}
		def := make(chan struct{}, 20)
		asyncSem.Store(&def)
	})

	// With the semaphore full, NotifyAsync must drop (hit default branch).
	// It should not block and should not spawn a goroutine.
	NotifyAsync(errors.New("should-drop"))

	// Verify the semaphore is still exactly full (1 token, capacity 1).
	// If NotifyAsync had somehow acquired a slot, len would be < cap.
	if len(ch) != cap(ch) {
		t.Errorf("expected semaphore to remain full (len=%d, cap=%d); NotifyAsync should have dropped", len(ch), cap(ch))
	}
}

func TestSetMaxAsyncNotifications_ConcurrentAccess(t *testing.T) {
	// Regression test: SetMaxAsyncNotifications and NotifyAsync must not
	// race on the asyncSem variable. Run with -race to verify.
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			SetMaxAsyncNotifications(50)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			NotifyAsync(errors.New("race test"))
		}
	}()
	wg.Wait()
}

func setupTestTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(old)
		tp.Shutdown(context.Background())
	})
	return exporter
}

func TestSetTraceId_SetsOTELAttribute(t *testing.T) {
	exporter := setupTestTracer(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")

	ctx = SetTraceId(ctx)
	expectedTraceID := GetTraceId(ctx)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "coldbrew.trace_id" {
			if attr.Value.AsString() != expectedTraceID {
				t.Errorf("coldbrew.trace_id = %q, want %q", attr.Value.AsString(), expectedTraceID)
			}
			found = true
		}
	}
	if !found {
		t.Error("coldbrew.trace_id attribute not found on span")
	}
}

func TestSetTraceId_EarlyReturn_SetsOTELAttribute(t *testing.T) {
	exporter := setupTestTracer(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")

	// Pre-set trace ID so SetTraceId takes the early return path
	ctx = options.AddToOptions(ctx, tracerID, "pre-existing-id")
	ctx = SetTraceId(ctx)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "coldbrew.trace_id" && attr.Value.AsString() == "pre-existing-id" {
			found = true
		}
	}
	if !found {
		t.Error("coldbrew.trace_id should be 'pre-existing-id' even on early return")
	}
}

func TestSetTraceId_MetadataPriority(t *testing.T) {
	exporter := setupTestTracer(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")

	// Inject gRPC metadata with a trace header — should take priority over OTEL span trace ID
	md := metadata.Pairs(traceHeader, "metadata-trace-id-123")
	ctx = metadata.NewIncomingContext(ctx, md)

	ctx = SetTraceId(ctx)
	traceID := GetTraceId(ctx)
	span.End()

	if traceID != "metadata-trace-id-123" {
		t.Errorf("expected metadata trace ID 'metadata-trace-id-123', got %q", traceID)
	}

	// Verify the attribute on the span matches the metadata trace ID
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "coldbrew.trace_id" && attr.Value.AsString() == "metadata-trace-id-123" {
			found = true
		}
	}
	if !found {
		t.Error("coldbrew.trace_id should be the metadata-supplied trace ID, not OTEL span trace ID")
	}
}

func TestSetTraceId_NoSpan_NoPanic(t *testing.T) {
	ctx := SetTraceId(context.Background())
	if GetTraceId(ctx) == "" {
		t.Error("expected a generated trace ID")
	}
}
