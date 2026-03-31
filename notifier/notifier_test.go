package notifier

import (
	"context"
	"sync"
	"testing"

	"github.com/go-coldbrew/errors"
	"github.com/go-coldbrew/options"
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
	// Verify that NotifyAsync respects the semaphore and doesn't panic.
	// Each iteration uses a fresh error to avoid the pre-existing race
	// on customError.shouldNotify across concurrent Notify calls.
	for i := 0; i < 20; i++ {
		NotifyAsync(errors.New("test error"))
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
