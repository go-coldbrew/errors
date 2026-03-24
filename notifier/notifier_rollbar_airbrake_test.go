package notifier

import (
	"testing"

	"github.com/go-coldbrew/errors"
)

func TestInitRollbar(t *testing.T) {
	rollbarInited = false
	t.Cleanup(func() { rollbarInited = false })

	InitRollbar("test-token", "test")
	if !rollbarInited {
		t.Error("expected rollbarInited to be true after InitRollbar")
	}
}

func TestInitAirbrake(t *testing.T) {
	old := airbrake
	t.Cleanup(func() { airbrake = old })

	airbrake = nil
	InitAirbrake(12345, "test-key")
	if airbrake == nil {
		t.Error("expected airbrake notifier to be non-nil after InitAirbrake")
	}
}

func TestRollbarNotifyDoesNotPanic(t *testing.T) {
	// Verify the notify path doesn't panic with rollbar disabled.
	// We avoid initializing rollbar here to prevent data races with
	// rollbar-go's internal async transport goroutine.
	rollbarInited = false
	t.Cleanup(func() { rollbarInited = false })

	err := errors.New("test rollbar error")
	Notify(err)
}

func TestRollbarNotifyOnPanicDoesNotPanic(t *testing.T) {
	rollbarInited = false
	t.Cleanup(func() { rollbarInited = false })

	func() {
		defer func() {
			recover()
		}()
		defer NotifyOnPanic()
		panic("test panic path")
	}()
}

func TestSetEnvironmentWithAirbrake(t *testing.T) {
	old := airbrake
	t.Cleanup(func() { airbrake = old })

	InitAirbrake(12345, "test-key")
	// Should not panic
	SetEnvironment("production")
}
