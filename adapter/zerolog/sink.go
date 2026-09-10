// Package zerologadapter bridges happycontext records into zerolog. A
// Sink serves the record's pre-encoded canonical line directly to plain
// loggers; context/hook/sampler-augmented loggers take the typed path.
package zerologadapter

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"time"
	"unsafe"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/bridge"
	"github.com/rs/zerolog"
)

// Sink writes happycontext records to zerolog.
type Sink struct {
	logger *zerolog.Logger
}

// New creates a zerolog-backed sink.
func New(l *zerolog.Logger) *Sink {
	if l != nil {
		checkLoggerLayout()
	}
	return &Sink{logger: l}
}

// loggerView mirrors zerolog.Logger's field layout, reached via unsafe
// pointer conversion, so the fast path can serve rec.Encoded() straight
// to the logger's writer, gated by its own level threshold. Guarded two
// ways: checkLoggerLayout validates the struct size at every New, and
// the view is only consulted for the fast-path decision — every other
// path goes through zerolog's public API.
//
// The layout is identical in zerolog v1.34.0 and v1.35.1 (both pinned
// to v1.35.1 here and in benches); only the first six fields are read.
type loggerView struct {
	w       zerolog.LevelWriter
	level   zerolog.Level
	sampler zerolog.Sampler
	context []byte
	hooks   []zerolog.Hook
	stack   bool
	ctx     context.Context
}

// checkLoggerLayout fails loudly if zerolog.Logger no longer has the
// layout loggerView mirrors. A size match does not prove field offsets,
// but a declaration-level change almost always moves the size, and the
// pinned go.mod version closes the remaining gap.
func checkLoggerLayout() {
	if unsafe.Sizeof(zerolog.Logger{}) != unsafe.Sizeof(loggerView{}) {
		panic("zerologadapter: zerolog.Logger layout changed; the direct-write fast path (loggerView) must be re-verified")
	}
}

// plain reports whether the logger is the zerolog.New(w) shape (at most
// a level filter): no contextual fields, non-timestamp hooks, sampler,
// or caller-stack state. Such loggers add nothing around the record, so
// rec.Encoded() is byte-for-byte the event they should emit; loggers
// built with With()/Hook()/Sample() augment every event and take the
// typed path. The view is re-read on every Write, so mutating
// *s.logger between writes is observed.
//
// A logger whose only augmentation is the Timestamp() hook counts as
// plain: the canonical line already carries the timestamp those hooks
// stamp, and the fast path bypasses hooks entirely, so serving it is
// the duplicate-free rendering of that shape.
func (v *loggerView) plain() bool {
	return v.w != nil && v.sampler == nil && len(v.context) <= 1 && onlyTimestampHooks(v.hooks) && !v.stack
}

// timestampHookSample is zerolog's unexported Timestamp() hook
// (context.go: type timestampHook struct{}), captured through the
// public API: a probe logger's hook slice, read via loggerView, holds
// the singleton zerolog installs for every .With().Timestamp().
// Interface comparison then detects it the way zerolog's own slog
// bridge detects it internally (hasTimestampHook). A nil probe (layout
// drift) conservatively disables both detection paths.
var timestampHookSample = func() (hook zerolog.Hook) {
	l := zerolog.New(nil).With().Timestamp().Logger()
	view := (*loggerView)(unsafe.Pointer(&l))
	if len(view.hooks) > 0 {
		hook = view.hooks[0]
	}
	return hook
}()

// onlyTimestampHooks reports whether every hook is the Timestamp()
// hook (or the slice is empty).
func onlyTimestampHooks(hooks []zerolog.Hook) bool {
	for _, h := range hooks {
		if timestampHookSample == nil || h != timestampHookSample {
			return false
		}
	}
	return true
}

// hasTimestampHook reports whether any hook is the Timestamp() hook.
func hasTimestampHook(hooks []zerolog.Hook) bool {
	if timestampHookSample == nil {
		return false
	}
	return slices.Contains(hooks, timestampHookSample)
}

// enabled mirrors zerolog's own gate (Logger.should) for a plain
// logger: written when the level is at or above both the logger's
// threshold and the package-global threshold. Logger-level samplers
// take the typed path, where zerolog applies them natively.
func (v *loggerView) enabled(level hc.Level) bool {
	return zlvlFor(level) >= v.level && zlvlFor(level) >= zerolog.GlobalLevel()
}

// canonicalSettings reports whether every zerolog global that shapes a
// rendered line is at the value the canonical bytes assume: member
// names, the time format, the duration unit and integer mode, the
// level name values, the level marshaller, and the timestamp function.
// When any global is customized, the fast path would emit bytes the
// user's pipeline does not expect — the typed path, which honors the
// customization through zerolog's own constructors, takes over.
//
// zerolog's defaults: TimeFieldFormat is RFC3339 (the canonical line's
// format), DurationFieldUnit is the millisecond, and DurationFieldInteger
// is false. The default LevelFieldMarshalFunc calls Level.String,
// which reads the exported Level*Value vars — so those vars are checked
// directly; a customized marshaller is caught by code pointer. Function
// values cannot be compared with ==, so the two function globals are
// identified by code pointer against the defaults captured at package
// init (a nil or replaced function fails the check).
func canonicalSettings() bool {
	return zerolog.LevelFieldName == "level" &&
		zerolog.TimestampFieldName == "time" &&
		zerolog.MessageFieldName == "message" &&
		zerolog.TimeFieldFormat == time.RFC3339 &&
		zerolog.DurationFieldUnit == time.Millisecond &&
		!zerolog.DurationFieldInteger &&
		zerolog.LevelDebugValue == "debug" &&
		zerolog.LevelInfoValue == "info" &&
		zerolog.LevelWarnValue == "warn" &&
		zerolog.LevelErrorValue == "error" &&
		funcPointer(zerolog.LevelFieldMarshalFunc) == defaultLevelFieldMarshalFunc &&
		funcPointer(zerolog.TimestampFunc) == defaultTimestampFunc
}

var (
	defaultLevelFieldMarshalFunc = funcPointer(zerolog.LevelFieldMarshalFunc)
	defaultTimestampFunc         = funcPointer(zerolog.TimestampFunc)
)

// funcPointer identifies a function value by code pointer. It returns 0
// for a nil or non-function value, which never matches a captured
// default.
func funcPointer(fn any) uintptr {
	v := reflect.ValueOf(fn)
	if !v.IsValid() || v.Kind() != reflect.Func {
		return 0
	}
	return v.Pointer()
}

// writeEncoded serves the record's pre-encoded canonical JSON line
// directly to the logger's writer — the bridge fast path (ledger:
// "zerolog bridge may serve rec.Encoded() directly"). It reports
// whether the fast path handled the record.
//
// Deliberate trade-offs: the line is hc's canonical line, byte-
// identical to the first-party JSON sink, served via one WriteLevel
// per record so level-aware writers keep working; errors route through
// zerolog.ErrorHandler; customized rendering globals and augmented
// loggers are rejected (canonicalSettings, plain) and take the typed
// path.
func (s *Sink) writeEncoded(view *loggerView, rec *hc.Record) bool {
	if !view.plain() || !view.enabled(rec.Level()) || !canonicalSettings() {
		return false
	}
	if _, err := view.w.WriteLevel(zlvlFor(rec.Level()), rec.Encoded()); err != nil {
		if zerolog.ErrorHandler != nil {
			zerolog.ErrorHandler(err)
		} else {
			fmt.Fprintf(os.Stderr, "zerolog: could not write event: %v\n", err)
		}
	}
	return true
}

// Write implements hc.Sink. Plain loggers receive the record's
// pre-encoded canonical line directly (writeEncoded); loggers that
// carry zerolog context/hooks/samplers fall back to the typed path:
// the record's fields are appended in insertion order (last-write-wins
// duplicates resolved) through zerolog's typed constructors.
func (s *Sink) Write(ctx context.Context, rec *hc.Record) {
	if s == nil || s.logger == nil || rec == nil {
		return
	}
	view := (*loggerView)(unsafe.Pointer(s.logger))
	if s.writeEncoded(view, rec) {
		return
	}

	event := s.eventFor(rec.Level())
	if !event.Enabled() {
		return
	}

	fields := rec.Fields()
	for _, i := range bridge.LastIndices(fields, hc.Field.WireKey) {
		event = appendField(event, fields[i])
	}
	// Stamp the record's own completion time (rec.Time) rather than a
	// fresh write-time read, so the typed path stays symmetric with
	// the fast path and the canonical line. Use the live global so
	// customized TimestampFieldName is honored (the fast path already
	// refused to run when the globals are not the defaults). Skip the
	// stamp when the logger stamps time itself (.With().Timestamp()):
	// the hook fires at Msg and would duplicate the member — the same
	// guard zerolog's own slog bridge applies.
	if !hasTimestampHook(view.hooks) {
		event.Time(zerolog.TimestampFieldName, rec.Time())
	}
	event.Msg(rec.Message())
}

func (s *Sink) eventFor(level hc.Level) *zerolog.Event {
	switch level {
	case hc.LevelDebug:
		return s.logger.Debug()
	case hc.LevelWarn:
		return s.logger.Warn()
	case hc.LevelError:
		return s.logger.Error()
	default:
		return s.logger.Info()
	}
}

// zlvlFor maps an hc level to the zerolog level carrying the same
// severity — used for WriteLevel routing and the threshold gate. The
// mapping matches eventFor's switch (unknown levels are info).
func zlvlFor(level hc.Level) zerolog.Level {
	switch level {
	case hc.LevelDebug:
		return zerolog.DebugLevel
	case hc.LevelWarn:
		return zerolog.WarnLevel
	case hc.LevelError:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// appendField maps a typed record field to zerolog's constructor — the
// mapping the v0 adapter used (error → message string, duration →
// float milliseconds via zerolog defaults, time → RFC3339 string).
func appendField(event *zerolog.Event, f hc.Field) *zerolog.Event {
	// WireKey matches Encoded(): colliding envelope keys become fields.*.
	key := f.WireKey()
	if str, ok := f.Str(); ok {
		return event.Str(key, str)
	}
	if i, ok := f.Int(); ok {
		return event.Int64(key, i)
	}
	if u, ok := f.Uint(); ok {
		return event.Uint64(key, u)
	}
	if fl, ok := f.Float(); ok {
		if f.Kind() == hc.KindFloat32 {
			return event.Float32(key, float32(fl))
		}
		return event.Float64(key, fl)
	}
	if b, ok := f.Bool(); ok {
		return event.Bool(key, b)
	}
	if tm, ok := f.Time(); ok {
		return event.Time(key, tm)
	}
	if d, ok := f.Duration(); ok {
		return event.Dur(key, d)
	}
	if err, ok := f.Err(); ok {
		return event.Str(key, bridge.ErrorMessage(err))
	}
	return event.Interface(key, f.Any())
}

var _ hc.Sink = (*Sink)(nil)
