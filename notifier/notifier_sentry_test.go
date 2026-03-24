package notifier

import (
	"sync"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/go-coldbrew/errors"
)

// capturedEvents collects sentry events via BeforeSend for test assertions.
type capturedEvents struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (c *capturedEvents) add(event *sentry.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *capturedEvents) last() *sentry.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return nil
	}
	return c.events[len(c.events)-1]
}

func (c *capturedEvents) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

// initTestSentry initializes sentry with a BeforeSend hook that captures events
// instead of sending them over the network.
func initTestSentry(t *testing.T) *capturedEvents {
	t.Helper()
	captured := &capturedEvents{}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://examplePublicKey@o0.ingest.sentry.io/0",
		Transport: &sentry.HTTPSyncTransport{},
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			captured.add(event)
			return nil // don't actually send
		},
	})
	if err != nil {
		t.Fatalf("failed to init sentry for test: %v", err)
	}
	sentryInited = true
	t.Cleanup(func() {
		sentryInited = false
		sentryEnvironment = ""
		sentryRelease = ""
	})
	return captured
}

func TestInitSentry_ValidDSN(t *testing.T) {
	sentryInited = false
	t.Cleanup(func() { sentryInited = false })
	InitSentry("https://examplePublicKey@o0.ingest.sentry.io/0")
	if !sentryInited {
		t.Error("expected sentryInited to be true after valid DSN")
	}
}

func TestInitSentry_InvalidDSN(t *testing.T) {
	sentryInited = false
	InitSentry("not-a-valid-dsn")
	if sentryInited {
		t.Error("expected sentryInited to be false after invalid DSN")
	}
}

func TestConvToSentry(t *testing.T) {
	err := errors.New("test error")
	errExt := err.(errors.ErrorExt)

	st := convToSentry(errExt)
	if st == nil {
		t.Fatal("expected non-nil stacktrace")
	}
	if len(st.Frames) == 0 {
		t.Fatal("expected at least one frame")
	}

	// Oldest frame should be first (bottom of stack)
	// The last frame should be closest to this test function
	lastFrame := st.Frames[len(st.Frames)-1]
	if lastFrame.Function == "" {
		t.Error("expected non-empty Function on last frame")
	}
	if lastFrame.Filename == "" {
		t.Error("expected non-empty Filename on last frame")
	}
	if lastFrame.Lineno == 0 {
		t.Error("expected non-zero Lineno on last frame")
	}
	if !lastFrame.InApp {
		t.Error("expected InApp to be true")
	}
}

func TestSentryLevelMapping(t *testing.T) {
	captured := initTestSentry(t)

	tests := []struct {
		level    string
		expected sentry.Level
	}{
		{"critical", sentry.LevelFatal},
		{"warning", sentry.LevelWarning},
		{"error", sentry.LevelError},
		{"", sentry.LevelError},
	}

	for _, tc := range tests {
		captured.reset()
		NotifyWithLevel(errors.New("test"), tc.level)
		event := captured.last()
		if event == nil {
			t.Fatalf("expected captured event for level %q", tc.level)
		}
		if event.Level != tc.expected {
			t.Errorf("level %q: got %v, want %v", tc.level, event.Level, tc.expected)
		}
	}
}

func TestSentryTags(t *testing.T) {
	captured := initTestSentry(t)

	tags := Tags{"method": "TestService.Get", "duration": "100ms"}
	Notify(errors.New("test error"), tags)

	event := captured.last()
	if event == nil {
		t.Fatal("expected captured event")
	}
	if event.Tags["method"] != "TestService.Get" {
		t.Errorf("expected tag method=TestService.Get, got %v", event.Tags["method"])
	}
	if event.Tags["duration"] != "100ms" {
		t.Errorf("expected tag duration=100ms, got %v", event.Tags["duration"])
	}
}

func TestSentryEnvironmentRelease(t *testing.T) {
	captured := initTestSentry(t)

	SetEnvironment("staging")
	SetRelease("v1.2.3")

	Notify(errors.New("test error"))

	event := captured.last()
	if event == nil {
		t.Fatal("expected captured event")
	}
	if event.Environment != "staging" {
		t.Errorf("expected environment=staging, got %v", event.Environment)
	}
	if event.Release != "v1.2.3" {
		t.Errorf("expected release=v1.2.3, got %v", event.Release)
	}
}

func TestSentryExtra(t *testing.T) {
	captured := initTestSentry(t)

	Notify(errors.New("test error"), "some extra data")

	event := captured.last()
	if event == nil {
		t.Fatal("expected captured event")
	}
	if len(event.Extra) == 0 {
		t.Error("expected non-empty Extra on event")
	}
}

func TestNotifyOnPanicSentry(t *testing.T) {
	captured := initTestSentry(t)

	func() {
		defer func() {
			// NotifyOnPanic re-panics, so we catch it here
			recover()
		}()
		defer NotifyOnPanic()
		panic("test panic")
	}()

	event := captured.last()
	if event == nil {
		t.Fatal("expected captured event from panic")
	}
	if event.Level != sentry.LevelFatal {
		t.Errorf("expected LevelFatal for panic, got %v", event.Level)
	}
}

func TestCloseSentry(t *testing.T) {
	initTestSentry(t)
	// Close should not panic
	Close()
}

func TestBuildSentryEvent(t *testing.T) {
	err := errors.New("build event test")
	errExt := err.(errors.ErrorExt)

	extra := map[string]interface{}{"key": "value"}
	tagData := []map[string]string{{"tag1": "val1"}}

	event := buildSentryEvent(errExt, "warning", extra, tagData)

	if event.Level != sentry.LevelWarning {
		t.Errorf("expected LevelWarning, got %v", event.Level)
	}
	if event.Message != "build event test" {
		t.Errorf("expected message 'build event test', got %v", event.Message)
	}
	if event.Extra["key"] != "value" {
		t.Errorf("expected extra key=value, got %v", event.Extra["key"])
	}
	if event.Tags["tag1"] != "val1" {
		t.Errorf("expected tag tag1=val1, got %v", event.Tags["tag1"])
	}
	if len(event.Exception) != 1 {
		t.Fatalf("expected 1 exception, got %d", len(event.Exception))
	}
	if event.Exception[0].Stacktrace == nil {
		t.Error("expected non-nil stacktrace in exception")
	}
}
