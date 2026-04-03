package notifier

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gobrake "github.com/airbrake/gobrake/v5"
	"github.com/getsentry/sentry-go"
	"github.com/go-coldbrew/errors"
	"github.com/go-coldbrew/log"
	"github.com/go-coldbrew/log/loggers"
	"github.com/go-coldbrew/options"
	"github.com/google/uuid"
	rollbar "github.com/rollbar/rollbar-go"
	otelattr "go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// Compile-time version compatibility check.
var _ = log.SupportPackageIsVersion1

var (
	airbrake           *gobrake.Notifier
	rollbarInited      bool
	sentryInited       bool
	sentryEnvironment  string
	sentryRelease      string
	serverRoot         string
	hostname           string
	traceHeader        string = "x-trace-id"

)

// asyncSem is a semaphore that bounds the number of concurrent async
// notification goroutines. When full, new notifications are dropped
// to prevent goroutine explosion under sustained error bursts.
// Stored as atomic.Pointer to eliminate the race between SetMaxAsyncNotifications
// and NotifyAsync goroutines reading the channel variable.
var asyncSem atomic.Pointer[chan struct{}]

func init() {
	ch := make(chan struct{}, 20)
	asyncSem.Store(&ch)
}

const (
	tracerID = "tracerId"
)

var asyncSemOnce sync.Once

// SetMaxAsyncNotifications sets the maximum number of concurrent async
// notification goroutines. When the limit is reached, new async notifications
// are dropped to prevent goroutine explosion under sustained error bursts.
// Default is 20. The first successful call wins; subsequent calls are no-ops.
// It is safe to call concurrently with NotifyAsync.
func SetMaxAsyncNotifications(n int) {
	if n > 0 {
		asyncSemOnce.Do(func() {
			ch := make(chan struct{}, n)
			asyncSem.Store(&ch)
		})
	}
}

// NotifyAsync sends an error notification asynchronously with bounded concurrency.
// If the async notification pool is full, the notification is dropped to prevent
// goroutine explosion under sustained error bursts.
// Returns the original error for convenience.
func NotifyAsync(err error, rawData ...interface{}) error {
	if err == nil {
		return nil
	}
	sem := *asyncSem.Load()
	select {
	case sem <- struct{}{}:
		data := append([]interface{}(nil), rawData...)
		go func(s chan struct{}, d []interface{}) {
			defer func() { <-s }()
			_ = Notify(err, d...)
		}(sem, data)
	default:
		// drop notification to prevent goroutine explosion
		log.Debug(context.Background(), "msg", "async notification dropped due to capacity", "err", err)
	}
	return err
}

// SetTraceHeaderName sets the header name for trace id
// default is x-trace-id
func SetTraceHeaderName(name string) {
	traceHeader = name
}

// GetTraceHeaderName gets the header name for trace id
// default is x-trace-id
func GetTraceHeaderName() string {
	return traceHeader
}

type isTags interface {
	isTags()
	value() map[string]string
}

type Tags map[string]string

func (tags Tags) isTags() {}

func (tags Tags) value() map[string]string {
	return map[string]string(tags)
}

// InitAirbrake inits airbrake configuration
// projectID: airbrake project id
// projectKey: airbrake project key
func InitAirbrake(projectID int64, projectKey string) {
	airbrake = gobrake.NewNotifierWithOptions(&gobrake.NotifierOptions{
		ProjectId:   projectID,
		ProjectKey:  projectKey,
		Environment: sentryEnvironment,
	})
}

// InitRollbar inits rollbar configuration
// token: rollbar token
// env: rollbar environment
func InitRollbar(token, env string) {
	rollbar.SetToken(token)
	rollbar.SetEnvironment(env)
	rollbar.SetStackTracer(func(err error) ([]runtime.Frame, bool) {
		if ext, ok := err.(errors.ErrorExt); ok {
			pcs := ext.Callers()
			if len(pcs) == 0 {
				return nil, false
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
				return nil, false
			}
			return frames, true
		}
		return nil, false
	})
	rollbarInited = true
}

// InitSentry inits sentry configuration
// dsn: sentry dsn
func InitSentry(dsn string) {
	sentryInited = false
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: sentryEnvironment,
		Release:     sentryRelease,
	}); err != nil {
		log.Error(context.Background(), "msg", "failed to init sentry", "err", err)
		return
	}
	sentryInited = true
}

func convToGoBrake(in []errors.StackFrame) []gobrake.StackFrame {
	out := make([]gobrake.StackFrame, 0)
	for _, s := range in {
		out = append(out, gobrake.StackFrame{
			File: s.File,
			Func: s.Func,
			Line: s.Line,
		})
	}
	return out
}

func convToSentry(in errors.ErrorExt) *sentry.Stacktrace {
	pcs := in.Callers()
	frames := make([]sentry.Frame, 0, len(pcs))

	callersFrames := runtime.CallersFrames(pcs)

	for {
		fr, more := callersFrames.Next()
		if fr.Func != nil {
			module := fr.Function
			function := fr.Function
			if idx := strings.LastIndex(fr.Function, "/"); idx != -1 {
				// Split "github.com/pkg.Func" into module and function
				rest := fr.Function[idx+1:]
				if dotIdx := strings.Index(rest, "."); dotIdx != -1 {
					module = fr.Function[:idx+1+dotIdx]
					function = rest[dotIdx+1:]
				}
			} else if idx := strings.Index(fr.Function, "."); idx != -1 {
				module = fr.Function[:idx]
				function = fr.Function[idx+1:]
			}
			frames = append(frames, sentry.Frame{
				Function: function,
				Module:   module,
				Filename: fr.File,
				AbsPath:  fr.File,
				Lineno:   fr.Line,
				InApp:    true,
			})
		}
		if !more {
			break
		}
	}
	// Reverse: sentry expects oldest frame first (bottom of stack)
	for i := len(frames)/2 - 1; i >= 0; i-- {
		opp := len(frames) - 1 - i
		frames[i], frames[opp] = frames[opp], frames[i]
	}
	return &sentry.Stacktrace{Frames: frames}
}

// parseRawData parses raw data to extra data and tags
func parseRawData(ctx context.Context, rawData ...interface{}) (extraData map[string]interface{}, tagData []map[string]string) {
	extraData = make(map[string]interface{})

	for pos := range rawData {
		data := rawData[pos]
		if _, ok := data.(context.Context); ok {
			continue
		}
		if tags, ok := data.(isTags); ok {
			tagData = append(tagData, tags.value())
		} else {
			extraData[reflect.TypeOf(data).String()+strconv.Itoa(pos)] = data
		}
	}
	if logFields := loggers.FromContext(ctx); logFields != nil {
		logFields.Range(func(k, v interface{}) bool {
			if str, ok := k.(string); ok {
				extraData[str] = v
			}
			return true
		})
	}
	return
}

// Notify notifies error to airbrake, rollbar and sentry if they are inited and error is not ignored
// err: error to notify
// rawData: extra data to notify with error (can be context.Context, Tags, or any other data)
// when rawData is context.Context, it will used to get extra data from loggers.FromContext(ctx) and tags from metadata
func Notify(err error, rawData ...interface{}) error {
	return NotifyWithLevelAndSkip(err, 2, rollbar.ERR, rawData...)
}

// NotifyWithLevel notifies error to airbrake, rollbar and sentry if they are inited and error is not ignored
// err: error to notify
// level: error level
// rawData: extra data to notify with error (can be context.Context, Tags, or any other data)
// when rawData is context.Context, it will used to get extra data from loggers.FromContext(ctx) and tags from metadata
func NotifyWithLevel(err error, level string, rawData ...interface{}) error {
	return NotifyWithLevelAndSkip(err, 2, level, rawData...)
}

func buildSentryEvent(err errors.ErrorExt, level string, extra map[string]interface{}, tagData []map[string]string) *sentry.Event {
	var sentryLevel sentry.Level
	switch level {
	case "critical":
		sentryLevel = sentry.LevelFatal
	case "warning":
		sentryLevel = sentry.LevelWarning
	default:
		sentryLevel = sentry.LevelError
	}

	event := &sentry.Event{
		Message:     err.Error(),
		Level:       sentryLevel,
		Environment: sentryEnvironment,
		Release:     sentryRelease,
		Extra:       extra,
		Exception: []sentry.Exception{
			{
				Type:       reflect.TypeOf(err).String(),
				Value:      err.Error(),
				Stacktrace: convToSentry(err),
			},
		},
	}

	if len(tagData) > 0 {
		tags := make(map[string]string)
		for _, t := range tagData {
			for k, v := range t {
				tags[k] = v
			}
		}
		if len(tags) > 0 {
			event.Tags = tags
		}
	}

	return event
}

// NotifyWithLevelAndSkip notifies error to airbrake, rollbar and sentry if they are inited and error is not ignored
// err: error to notify
// skip: skip stack frames when notify error
// level: error level
// rawData: extra data to notify with error (can be context.Context, Tags, or any other data)
// when rawData is context.Context, it will used to get extra data from loggers.FromContext(ctx) and tags from metadata
func NotifyWithLevelAndSkip(err error, skip int, level string, rawData ...interface{}) error {
	if err == nil {
		return nil
	}

	if n, ok := err.(errors.NotifyExt); ok {
		if !n.ShouldNotify() {
			return err
		}
		n.Notified(true)
	}
	return doNotify(err, skip, level, rawData...)

}

func doNotify(err error, skip int, level string, rawData ...interface{}) error {
	if err == nil {
		return nil
	}

	// add stack infomation
	errWithStack, ok := err.(errors.ErrorExt)
	if !ok {
		errWithStack = errors.WrapWithSkip(err, "", skip+1)
	}

	list := make([]interface{}, 0)
	for pos := range rawData {
		data := rawData[pos]
		// if we find the error, return error and do not log it
		if e, ok := data.(error); ok {
			if e == err {
				return err
			} else if er, ok := e.(errors.ErrorExt); ok {
				if err == er.Cause() {
					return err
				}
			}
		} else {
			list = append(list, rawData[pos])
		}
	}

	// try to fetch a correlation ID and OTEL trace ID from rawData
	var correlationID string
	var otelTraceID string
	ctx := context.Background()
	for _, d := range list {
		if c, ok := d.(context.Context); ok {
			// Application-level correlation ID (set via SetTraceId) takes precedence.
			correlationID = GetTraceId(c)
			// OTEL distributed trace ID is captured separately for linking.
			if span := oteltrace.SpanFromContext(c); span.SpanContext().IsValid() {
				otelTraceID = span.SpanContext().TraceID().String()
				if strings.TrimSpace(correlationID) == "" {
					correlationID = otelTraceID
				}
			}
			ctx = c
			break
		}
	}

	if airbrake != nil {
		n := gobrake.NewNotice(errWithStack, nil, 1)
		n.Errors[0].Backtrace = convToGoBrake(errWithStack.StackFrame())
		if len(list) > 0 {
			m, _ := parseRawData(ctx, list...)
			for k, v := range m {
				n.Context[k] = v
			}
		}
		if correlationID != "" {
			n.Context["traceId"] = correlationID
		}
		if otelTraceID != "" {
			n.Context["otelTraceId"] = otelTraceID
		}
		airbrake.SendNoticeAsync(n)
	}

	parsedData, tagData := parseRawData(ctx, list...)
	if rollbarInited {
		extras := make(map[string]interface{})
		for k, v := range parsedData {
			extras[k] = v
		}
		if correlationID != "" {
			extras["traceId"] = correlationID
		}
		if otelTraceID != "" {
			extras["otelTraceId"] = otelTraceID
		}
		extras["server"] = map[string]interface{}{"hostname": getHostname(), "root": getServerRoot()}
		rollbar.ErrorWithStackSkipWithExtras(level, errWithStack, skip+1, extras)
	}

	if sentryInited {
		event := buildSentryEvent(errWithStack, level, parsedData, tagData)
		sentry.CaptureEvent(event)
	}

	log.GetLogger().Log(ctx, loggers.ErrorLevel, skip+1, "err", errWithStack, "stack", errWithStack.StackFrame())
	return err
}

// NotifyWithExclude notifies error to airbrake, rollbar and sentry if they are inited and error is not ignored
// err: error to notify
// rawData: extra data to notify with error (can be context.Context, Tags, or any other data)
// when rawData is context.Context, it will used to get extra data from loggers.FromContext(ctx) and tags from metadata
func NotifyWithExclude(err error, rawData ...interface{}) error {
	if err == nil {
		return nil
	}

	list := make([]interface{}, 0)
	for pos := range rawData {
		data := rawData[pos]
		// if we find the error, return error and do not log it
		if e, ok := data.(error); ok {
			if er, ok := e.(errors.ErrorExt); ok {
				if err == er.Cause() {
					return err
				} else if er == err {
					return err
				}
			}
		} else {
			list = append(list, rawData[pos])
		}
	}
	_ = NotifyAsync(err, list...)
	return err
}

// NotifyOnPanic notifies error to airbrake, rollbar and sentry if they are inited and error is not ignored
// rawData: extra data to notify with error (can be context.Context, Tags, or any other data)
// when rawData is context.Context, it will used to get extra data from loggers.FromContext(ctx) and tags from metadata
// this function should be called in defer
// example: defer NotifyOnPanic(ctx, "some data")
// example: defer NotifyOnPanic(ctx, "some data", Tags{"tag1": "value1"})
func NotifyOnPanic(rawData ...interface{}) {
	if airbrake != nil {
		defer airbrake.NotifyOnPanic()
	}

	ctx := context.Background()
	for _, d := range rawData {
		if c, ok := d.(context.Context); ok {
			ctx = c
			break
		}
	}
	if r := recover(); r != nil {
		var e errors.ErrorExt
		switch val := r.(type) {
		case error:
			e = errors.WrapWithSkip(val, "PANIC", 1)
		case string:
			e = errors.NewWithSkip("PANIC: "+val, 1)
		default:
			e = errors.NewWithSkip("Panic", 1)
		}
		parsedData, tagData := parseRawData(ctx, rawData...)
		if rollbarInited {
			rollbar.ErrorWithStackSkipWithExtras(rollbar.CRIT, e, 1, map[string]interface{}{"panic": r})
		}
		if sentryInited {
			event := buildSentryEvent(e, "critical", parsedData, tagData)
			sentry.CaptureEvent(event)
		}
		panic(e)
	}
}

// Close closes the airbrake notifier and flushes pending Sentry events.
// Sentry events are flushed with a 2 second timeout.
// You should call Close before app shutdown.
// Close doesn't call os.Exit.
func Close() {
	if airbrake != nil {
		airbrake.Close()
	}
	if sentryInited {
		sentry.Flush(2 * time.Second)
	}
}

// SetEnvironment sets the environment.
// The environment is used to distinguish errors occurring in different
func SetEnvironment(env string) {
	if airbrake != nil {
		airbrake.AddFilter(func(notice *gobrake.Notice) *gobrake.Notice {
			notice.Context["environment"] = env
			return notice
		})
	}
	rollbar.SetEnvironment(env)
	sentryEnvironment = env
}

// SetRelease sets the release tag.
// The release tag is used to group errors together by release.
func SetRelease(rel string) {
	sentryRelease = rel
}

// SetTraceIdWithValue is like SetTraceId but also returns the resolved trace ID,
// avoiding a separate GetTraceId call.
func SetTraceIdWithValue(ctx context.Context) (context.Context, string) {
	span := oteltrace.SpanFromContext(ctx)
	hasSpan := span.SpanContext().IsValid()

	if traceID := GetTraceId(ctx); traceID != "" {
		// Trace ID already set — ensure it's linked to the OTEL span.
		if hasSpan {
			span.SetAttributes(otelattr.String("coldbrew.trace_id", traceID))
		}
		return ctx, traceID
	}
	var traceID string
	// Check gRPC metadata first — client-supplied trace ID takes priority.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if id, ok := md["grpcmetadata-"+traceHeader]; ok {
			traceID = strings.Join(id, ",")
		} else if id, ok := md[traceHeader]; ok {
			traceID = strings.Join(id, ",")
		}
	}
	// Fall back to OTEL span trace ID.
	if strings.TrimSpace(traceID) == "" && hasSpan {
		traceID = span.SpanContext().TraceID().String()
	}
	// Last resort: generate UUID.
	if strings.TrimSpace(traceID) == "" {
		u, err := uuid.NewRandom()
		if err != nil {
			u, _ = uuid.NewUUID()
		}
		traceID = u.String()
	}
	// Link the resolved trace ID to the OTEL span as an attribute
	// so ColdBrew correlation ID and distributed trace are connected.
	if hasSpan {
		span.SetAttributes(otelattr.String("coldbrew.trace_id", traceID))
	}
	ctx = loggers.AddToLogContext(ctx, "trace", traceID)
	return options.AddToOptions(ctx, tracerID, traceID), traceID
}

// SetTraceId updates the traceID based on context values
// if no trace id is found then it will create one and update the context
// You should use the context returned by this function instead of the one passed
//
//go:inline
func SetTraceId(ctx context.Context) context.Context {
	ctx, _ = SetTraceIdWithValue(ctx)
	return ctx
}

// GetTraceId fetches traceID from context
// if no trace id is found then it will return empty string
func GetTraceId(ctx context.Context) string {
	if o := options.FromContext(ctx); o != nil {
		if data, found := o.Get(tracerID); found {
			if traceID, ok := data.(string); ok {
				return traceID
			}
		}
	}
	if logCtx := loggers.FromContext(ctx); logCtx != nil {
		if data, found := logCtx.Load("trace"); found {
			if traceID, ok := data.(string); ok {
				options.AddToOptions(ctx, tracerID, traceID)
				return traceID
			}
		}
	}
	return ""
}

// UpdateTraceId force updates the traced id to provided id
// if no trace id is found then it will create one and update the context
// You should use the context returned by this function instead of the one passed
func UpdateTraceId(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return SetTraceId(ctx)
	}
	ctx = loggers.AddToLogContext(ctx, "trace", traceID)
	return options.AddToOptions(ctx, tracerID, traceID)
}

// SetServerRoot sets the root directory of the project.
// The root directory is used to trim prefixes from filenames in stack traces.
func SetServerRoot(path string) {
	serverRoot = path
}

// SetHostname sets the hostname of the server.
// The hostname is used to identify the server that logged an error.
func SetHostname(name string) {
	hostname = name
}

func getHostname() string {
	if hostname != "" {
		return hostname
	}
	name, err := os.Hostname()
	if err == nil {
		hostname = name
	}
	return hostname
}

func getServerRoot() string {
	if serverRoot != "" {
		return serverRoot
	}
	cwd, err := os.Getwd()
	if err == nil {
		serverRoot = cwd
	}
	return serverRoot
}
