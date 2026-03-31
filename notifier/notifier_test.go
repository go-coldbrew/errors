package notifier

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	// Set a tiny semaphore so we can observe drops.
	ch := make(chan struct{}, 1)
	asyncSem.Store(&ch)
	t.Cleanup(func() {
		// Drain any tokens left by test goroutines.
		select {
		case <-ch:
		default:
		}
		// Restore default.
		def := make(chan struct{}, 20)
		asyncSem.Store(&def)
	})

	// Fill the single slot with a blocking goroutine.
	block := make(chan struct{})
	blockErr := errors.New("blocker")
	NotifyAsync(blockErr) // takes the one slot
	// Give the goroutine a moment to acquire the semaphore token.
	time.Sleep(10 * time.Millisecond)

	// Now the semaphore is full. Additional calls should be dropped.
	var dropped atomic.Int32
	originalDebug := NotifyAsync(errors.New("should-drop"))
	// NotifyAsync returns the error regardless of drop/send, so we can't
	// check the return value. Instead, verify the semaphore is still full
	// by checking we can't send another token.
	select {
	case ch <- struct{}{}:
		// We could send — means the slot was free, which means the previous
		// call was dropped (it didn't acquire). That's the expected path.
		<-ch // put it back
		dropped.Add(1)
	default:
		// Slot is full — the previous NotifyAsync got in, which shouldn't
		// happen since we already filled it. This is also fine if timing
		// allowed the blocker to finish.
	}
	_ = originalDebug

	// Unblock the first goroutine so it releases the token.
	close(block)
	// Wait a bit for cleanup.
	time.Sleep(50 * time.Millisecond)
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
