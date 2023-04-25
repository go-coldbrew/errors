package notifier

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	raven "github.com/getsentry/raven-go"
	"github.com/go-coldbrew/errors"
	"github.com/go-coldbrew/log"
	"github.com/go-coldbrew/log/loggers"
	"github.com/go-coldbrew/options"
	"github.com/google/uuid"
	stdopentracing "github.com/opentracing/opentracing-go"
	"github.com/stvp/rollbar"
	"google.golang.org/grpc/metadata"
	gobrake "gopkg.in/airbrake/gobrake.v2"
)

var (
	airbrake      *gobrake.Notifier
	rollbarInited bool
	sentryInited  bool
	serverRoot    string
	hostname      string
	traceHeader   string = "x-trace-id"
)

const (
	tracerID = "tracerId"
)

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
	airbrake = gobrake.NewNotifier(projectID, projectKey)
}

// InitRollbar inits rollbar configuration
// token: rollbar token
// env: rollbar environment
func InitRollbar(token, env string) {
	rollbar.Token = token
	rollbar.Environment = env
	rollbarInited = true
}

// InitSentry inits sentry configuration
// dsn: sentry dsn
func InitSentry(dsn string) {
	raven.SetDSN(dsn)
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

func convToRollbar(in []errors.StackFrame) rollbar.Stack {
	out := rollbar.Stack{}
	for _, s := range in {
		out = append(out, rollbar.Frame{
			Filename: s.File,
			Method:   s.Func,
			Line:     s.Line,
		})
	}
	return out
}

func convToSentry(in errors.ErrorExt) *raven.Stacktrace {
	out := new(raven.Stacktrace)
	pcs := in.Callers()
	frames := make([]*raven.StacktraceFrame, 0)

	callersFrames := runtime.CallersFrames(pcs)

	for {
		fr, more := callersFrames.Next()
		if fr.Func != nil {
			frame := raven.NewStacktraceFrame(fr.PC, fr.Function, fr.File, fr.Line, 3, []string{})
			if frame != nil {
				frame.InApp = true
				frames = append(frames, frame)
			}
		}
		if !more {
			break
		}
	}
	for i := len(frames)/2 - 1; i >= 0; i-- {
		opp := len(frames) - 1 - i
		frames[i], frames[opp] = frames[opp], frames[i]
	}
	out.Frames = frames
	return out
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

	// try to fetch a traceID and context from rawData
	var traceID string
	ctx := context.Background()
	for _, d := range list {
		if c, ok := d.(context.Context); ok {
			if span := stdopentracing.SpanFromContext(c); span != nil {
				traceID = span.BaggageItem("trace")
			}
			if strings.TrimSpace(traceID) == "" {
				traceID = GetTraceId(c)
			}
			ctx = c
			break
		}
	}

	if airbrake != nil {
		var n *gobrake.Notice
		n = gobrake.NewNotice(errWithStack, nil, 1)
		n.Errors[0].Backtrace = convToGoBrake(errWithStack.StackFrame())
		if len(list) > 0 {
			m, _ := parseRawData(ctx, list...)
			for k, v := range m {
				n.Context[k] = v
			}
		}
		if traceID != "" {
			n.Context["traceId"] = traceID
		}
		airbrake.SendNoticeAsync(n)
	}

	parsedData, tagData := parseRawData(ctx, list...)
	if rollbarInited {
		fields := []*rollbar.Field{}
		if len(list) > 0 {
			for k, v := range parsedData {
				fields = append(fields, &rollbar.Field{Name: k, Data: v})
			}
		}
		if traceID != "" {
			fields = append(fields, &rollbar.Field{Name: "traceId", Data: traceID})
		}
		fields = append(fields, &rollbar.Field{Name: "server", Data: map[string]interface{}{"hostname": getHostname(), "root": getServerRoot()}})
		rollbar.ErrorWithStack(level, errWithStack, convToRollbar(errWithStack.StackFrame()), fields...)
	}

	if sentryInited {
		defLevel := raven.ERROR
		if level == "critical" {
			defLevel = raven.FATAL
		} else if level == "warning" {
			defLevel = raven.WARNING
		}
		ravenExp := raven.NewException(errWithStack, convToSentry(errWithStack))
		packet := raven.NewPacketWithExtra(errWithStack.Error(), parsedData, ravenExp)

		for _, tags := range tagData {
			packet.AddTags(tags)
		}

		packet.Level = defLevel
		raven.Capture(packet, nil)
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
	go Notify(err, list...)
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
			rollbar.ErrorWithStack(rollbar.CRIT, e, convToRollbar(e.StackFrame()), &rollbar.Field{Name: "panic", Data: r})
		}
		if sentryInited {
			ravenExp := raven.NewException(e, convToSentry(e))
			packet := raven.NewPacketWithExtra(e.Error(), parsedData, ravenExp)

			for _, tags := range tagData {
				packet.AddTags(tags)
			}

			packet.Level = raven.FATAL
			raven.Capture(packet, nil)
		}
		panic(e)
	}
}

// Close closes the airbrake notifier and flushes the error queue.
// You should call Close before app shutdown.
// Close doesn't call os.Exit.
func Close() {
	if airbrake != nil {
		airbrake.Close()
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
	rollbar.Environment = env
	raven.SetEnvironment(env)
}

// SetRelease sets the release tag.
// The release tag is used to group errors together by release.
func SetRelease(rel string) {
	raven.SetRelease(rel)
}

// SetTraceId updates the traceID based on context values
// if no trace id is found then it will create one and update the context
// You should use the context returned by this function instead of the one passed
func SetTraceId(ctx context.Context) context.Context {
	if GetTraceId(ctx) != "" {
		return ctx
	}
	var traceID string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if id, ok := md["grpcmetadata-"+traceHeader]; ok {
			traceID = strings.Join(id, ",")
		} else if id, ok := md[traceHeader]; ok {
			traceID = strings.Join(id, ",")
		}
	}
	if span := stdopentracing.SpanFromContext(ctx); span != nil && strings.TrimSpace(traceID) == "" {
		traceID = span.BaggageItem("trace")
	}
	// if no trace id then create one
	if strings.TrimSpace(traceID) == "" {
		u, err := uuid.NewRandom()
		if err != nil {
			u, _ = uuid.NewUUID()
		}
		traceID = u.String()
	}
	ctx = loggers.AddToLogContext(ctx, "trace", traceID)
	return options.AddToOptions(ctx, tracerID, traceID)
}

// GetTraceId fetches traceID from context
// if no trace id is found then it will return empty string
func GetTraceId(ctx context.Context) string {
	if o := options.FromContext(ctx); o != nil {
		if data, found := o.Get(tracerID); found {
			return data.(string)
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
