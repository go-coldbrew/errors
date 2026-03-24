package notifier

import (
	"runtime"
	"testing"

	"github.com/go-coldbrew/errors"
	rollbar "github.com/rollbar/rollbar-go"
)

func TestInitRollbar(t *testing.T) {
	rollbarInited = false
	t.Cleanup(func() {
		rollbarInited = false
		// Reset rollbar global state to safe defaults.
		// rollbar-go only exposes setters, not getters, so we can't
		// snapshot/restore. Setting empty token effectively disables sending.
		rollbar.SetToken("")
		rollbar.SetEnvironment("")
	})

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

func TestInitAirbrakeSeededEnvironment(t *testing.T) {
	old := airbrake
	oldEnv := sentryEnvironment
	t.Cleanup(func() {
		airbrake = old
		sentryEnvironment = oldEnv
	})

	sentryEnvironment = "staging"
	InitAirbrake(12345, "test-key")
	if airbrake == nil {
		t.Fatal("expected airbrake notifier to be non-nil")
	}
}

func TestRollbarStackTracerExtractsErrorExtFrames(t *testing.T) {
	// Verify that ErrorExt.Callers() can be converted to []runtime.Frame
	// This is the same logic registered via rollbar.SetStackTracer in InitRollbar.
	err := errors.New("test error with stack")
	ext, ok := err.(errors.ErrorExt)
	if !ok {
		t.Fatal("expected ErrorExt")
	}

	pcs := ext.Callers()
	if len(pcs) == 0 {
		t.Fatal("expected non-empty callers from ErrorExt")
	}

	frames := make([]runtime.Frame, 0, len(pcs))
	callersFrames := runtime.CallersFrames(pcs)
	for {
		f, more := callersFrames.Next()
		if f.Function != "" {
			frames = append(frames, f)
		}
		if !more {
			break
		}
	}

	if len(frames) == 0 {
		t.Fatal("expected non-empty runtime.Frame slice from ErrorExt callers")
	}

	// Verify frames contain meaningful data
	found := false
	for _, f := range frames {
		if f.Function != "" && f.File != "" && f.Line > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one frame with Function, File, and Line")
	}
}

func TestNotifyOnPanicRecoverable(t *testing.T) {
	rollbarInited = false
	t.Cleanup(func() { rollbarInited = false })

	func() {
		defer func() {
			// NotifyOnPanic re-panics — verify it's recoverable
			// and doesn't double-panic
			recover()
		}()
		defer NotifyOnPanic()
		panic("test panic path")
	}()
}

func TestNotifyDoesNotPanicWhenDisabled(t *testing.T) {
	rollbarInited = false
	t.Cleanup(func() { rollbarInited = false })

	err := errors.New("test rollbar error")
	Notify(err)
}

func TestSetEnvironmentWithAirbrake(t *testing.T) {
	old := airbrake
	t.Cleanup(func() { airbrake = old })

	InitAirbrake(12345, "test-key")
	SetEnvironment("production")
}
