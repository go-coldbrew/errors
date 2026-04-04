package notifier

import (
	"context"
	"strings"
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

func TestSetTraceIdWithValue_ReturnsTraceID(t *testing.T) {
	ctx, traceID := SetTraceIdWithValue(context.Background())
	if traceID == "" {
		t.Fatal("expected a non-empty trace ID")
	}
	if got := GetTraceId(ctx); got != traceID {
		t.Errorf("GetTraceId = %q, want %q", got, traceID)
	}
}

func TestSetTraceIdWithValue_ExistingTraceID(t *testing.T) {
	ctx := options.AddToOptions(context.Background(), tracerID, "existing-id")
	ctx, traceID := SetTraceIdWithValue(ctx)
	if traceID != "existing-id" {
		t.Errorf("expected existing-id, got %q", traceID)
	}
	if got := GetTraceId(ctx); got != "existing-id" {
		t.Errorf("GetTraceId = %q, want existing-id", got)
	}
}

func TestSetTraceIdWithValue_SetsOTELAttribute(t *testing.T) {
	exporter := setupTestTracer(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "test-span")

	ctx, traceID := SetTraceIdWithValue(ctx)
	span.End()

	if traceID == "" {
		t.Fatal("expected a non-empty trace ID")
	}
	if got := GetTraceId(ctx); got != traceID {
		t.Errorf("GetTraceId = %q, want %q", got, traceID)
	}

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "coldbrew.trace_id" && attr.Value.AsString() == traceID {
			found = true
		}
	}
	if !found {
		t.Error("coldbrew.trace_id attribute not found on span")
	}
}

func TestValidateTraceID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty stays empty", "", ""},
		{"normal ASCII passes through", "abc-123-def", "abc-123-def"},
		{"UUID passes through", "550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"truncated at 128 chars", strings.Repeat("a", 200), strings.Repeat("a", 128)},
		{"newlines stripped", "abc\ndef\rghi", "abcdefghi"},
		{"null bytes stripped", "abc\x00def", "abcdef"},
		{"control chars stripped", "abc\x01\x02\x03def", "abcdef"},
		{"tab stripped", "abc\tdef", "abcdef"},
		{"spaces preserved", "abc def", "abc def"},
		{"printable special chars preserved", "abc!@#$%^&*()def", "abc!@#$%^&*()def"},
		{"only non-printable returns empty", "\x00\x01\x02", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validateTraceID(tc.input)
			if got != tc.want {
				t.Errorf("validateTraceID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSetTraceId_ValidatesMetadataTraceID(t *testing.T) {
	md := metadata.Pairs(traceHeader, "valid-id-123\x00\nnewline")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = SetTraceId(ctx)
	got := GetTraceId(ctx)
	if got != "valid-id-123newline" {
		t.Errorf("expected sanitized trace ID %q, got %q", "valid-id-123newline", got)
	}
}

func TestSetTraceId_TruncatesLongMetadataTraceID(t *testing.T) {
	longID := strings.Repeat("x", 200)
	md := metadata.Pairs(traceHeader, longID)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	ctx = SetTraceId(ctx)
	got := GetTraceId(ctx)
	if len(got) != 128 {
		t.Errorf("expected truncated to 128 chars, got %d", len(got))
	}
}

func TestUpdateTraceId_ValidatesInput(t *testing.T) {
	ctx := options.AddToOptions(context.Background(), tracerID, "")
	ctx = UpdateTraceId(ctx, "good-id\x00\nbad")
	got := GetTraceId(ctx)
	if got != "good-idbad" {
		t.Errorf("expected sanitized trace ID %q, got %q", "good-idbad", got)
	}
}

func TestUpdateTraceId_AllInvalidFallsBackToGenerated(t *testing.T) {
	ctx := UpdateTraceId(context.Background(), "\x00\x01\x02")
	got := GetTraceId(ctx)
	if got == "" {
		t.Error("expected a generated trace ID when all chars are invalid")
	}
}
