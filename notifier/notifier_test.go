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
