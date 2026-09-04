package zerologadapter

import (
	"context"
	"fmt"
	"os"
	"unsafe"

	"github.com/happytoolin/happycontext"
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

// loggerView mirrors zerolog.Logger's field layout. zerolog exposes no
// API to reach a Logger's writer or filter state (its fields are
// unexported; Output() installs a writer rather than returning the
// current one), yet the bridge's fast path below needs exactly those:
// it serves the record's pre-encoded canonical line (rec.Encoded())
// straight to the logger's writer, gated by the logger's own level
// threshold. The view is reached with an unsafe pointer conversion and
// is guarded two ways:
//
//   - checkLoggerLayout (below) validates struct size against the
//     compiled zerolog at every New, so a future zerolog that changes
//     the Logger shape fails loudly at construction instead of reading
//     garbage field offsets;
//   - the view is only consulted for the fast-path decision; every
//     other code path goes through zerolog's public API.
//
// The layout below is identical in zerolog v1.34.0 (this module's
// pinned version) and v1.35.1 (the version benches resolve via MVS) —
// field order w, level, sampler, context, hooks, stack, ctx, verified
// against both sources. Only the first six fields are read.
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
// pinned go.mod version plus the two-version verification above close
// the remaining gap for every build this module ships.
func checkLoggerLayout() {
	if unsafe.Sizeof(zerolog.Logger{}) != unsafe.Sizeof(loggerView{}) {
		panic("zerologadapter: zerolog.Logger layout changed; the direct-write fast path (loggerView) must be re-verified")
	}
}

// plain reports whether the logger is the zerolog.New(w) shape (at most
// a level filter applied): no contextual fields, hooks, sampler, or
// caller-stack state. Such loggers add nothing around the record, so
// rec.Encoded() — the record's own canonical line — is byte-for-byte
// the event they should emit. Loggers built with With()/Hook()/Sample()
// augment every native event with members (context fields), decisions
// (samplers), or side effects (hooks) that a raw canonical line cannot
// reproduce; those take the typed path below, which runs the full
// native machinery. The view is re-read on every Write, so a caller
// mutating *z.logger between writes is observed.
func (v *loggerView) plain() bool {
	return v.w != nil && v.sampler == nil && len(v.context) <= 1 && len(v.hooks) == 0 && !v.stack
}

// enabled mirrors zerolog's own gate (Logger.should) for a plain
// logger: an event is written when its level is at or above both the
// logger's threshold and the package-global threshold. A plain logger
// carries no sampler, so should()'s sampler branch is structurally
// absent here; logger-level samplers take the typed path instead,
// where zerolog applies them natively.
func (v *loggerView) enabled(level hc.Level) bool {
	return zlvlFor(level) >= v.level && zlvlFor(level) >= zerolog.GlobalLevel()
}

// defaultFieldNames reports whether zerolog's member-name globals are
// still the defaults the canonical line writes. rec.Encoded() is a
// fixed line: its envelope members are always "level", "time", and
// "message". When the globals are customized, native events carry the
// custom names, so serving the canonical bytes would emit members the
// user's pipeline does not expect — the typed path (which honors the
// globals through zerolog's own constructors) must take over.
func defaultFieldNames() bool {
	return zerolog.LevelFieldName == "level" &&
		zerolog.TimestampFieldName == "time" &&
		zerolog.MessageFieldName == "message"
}

// writeEncoded serves the record's pre-encoded canonical JSON line
// directly to the logger's writer — the bridge fast path (ledger:
// "zerolog bridge may serve rec.Encoded() directly"). It reports
// whether the fast path handled the record.
//
// Trade-offs versus the typed path, all deliberate and documented:
//
//   - The line is hc's canonical line, not a zerolog-assembled event:
//     field names and shapes follow hc's wire contract (level, message,
//     RFC3339 time), which is byte-identical to the first-party JSON
//     sink's output. When zerolog's member-name globals (LevelFieldName,
//     TimestampFieldName, MessageFieldName) are customized, the fast
//     path is skipped (defaultFieldNames) and the typed path emits the
//     customized names.
//   - The writer sees one WriteLevel call per record with the full
//     line, exactly like a native event's write — level-aware writers
//     (MultiLevelWriter, FilteredLevelWriter, ...) keep working.
//   - Write errors route through zerolog.ErrorHandler, mirroring
//     native event writes.
//   - Contextual/hook/sampler-augmented loggers never take this path
//     (see plain), preserving their native augmentation.
func (z *Sink) writeEncoded(rec *hc.Record) bool {
	view := (*loggerView)(unsafe.Pointer(z.logger))
	if !view.plain() || !view.enabled(rec.Level()) || !defaultFieldNames() {
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
// duplicates resolved) through zerolog's typed constructors — the same
// field shapes the v0 adapter produced, with native augmentation.
func (z *Sink) Write(ctx context.Context, rec *hc.Record) {
	if z == nil || z.logger == nil || rec == nil {
		return
	}
	if z.writeEncoded(rec) {
		return
	}

	event := z.eventFor(rec.Level())
	if !event.Enabled() {
		return
	}

	fields := rec.Fields()
	for _, i := range lastOccurrences(fields) {
		event = appendField(event, fields[i])
	}
	// Stamp the record's own completion time (rec.Time) rather than a
	// fresh write-time read: the fast path and the canonical line carry
	// completedAt, so the typed path stays symmetric with them.
	event.Time("time", rec.Time())
	event.Msg(rec.Message())
}

func (z *Sink) eventFor(level hc.Level) *zerolog.Event {
	switch level {
	case hc.LevelDebug:
		return z.logger.Debug()
	case hc.LevelWarn:
		return z.logger.Warn()
	case hc.LevelError:
		return z.logger.Error()
	default:
		return z.logger.Info()
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
// identical mapping the v0 adapter used (error → message string,
// duration → float milliseconds via zerolog defaults, time → RFC3339
// string), with RawJSON appending pre-encoded bytes verbatim.
func appendField(event *zerolog.Event, f hc.Field) *zerolog.Event {
	key := f.Key()
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
		return event.Str(key, err.Error())
	}
	if raw, ok := f.Raw(); ok {
		return event.RawJSON(key, raw)
	}
	return event.Interface(key, f.Any())
}

// lastOccurrences returns the indices of each key's last write, in
// forward emission order — the same duplicate resolution the core
// encoder applies (linear scan for narrow events, backward seen-set
// collection for wide ones).
func lastOccurrences(fields []hc.Field) []int {
	if len(fields) <= 24 {
		var stack [24]int // allocation-free narrow path
		n := 0
		for i := range fields {
			last := true
			for j := i + 1; j < len(fields); j++ {
				if fields[j].Key() == fields[i].Key() {
					last = false
					break
				}
			}
			if last {
				stack[n] = i
				n++
			}
		}
		return stack[:n:n]
	}
	seen := make(map[string]struct{}, len(fields)*2)
	kept := make([]int, 0, len(fields))
	for i := len(fields) - 1; i >= 0; i-- {
		if _, dup := seen[fields[i].Key()]; dup {
			continue
		}
		seen[fields[i].Key()] = struct{}{}
		kept = append(kept, i)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

var _ hc.Sink = (*Sink)(nil)
