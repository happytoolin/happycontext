package zapadapter

import (
	"context"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Sink writes happycontext records to zap.
type Sink struct {
	logger *zap.Logger
}

// New creates a zap-backed sink.
func New(l *zap.Logger) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink: the record's fields are appended in
// insertion order (last-write-wins duplicates resolved) as typed zap
// fields.
func (z *Sink) Write(ctx context.Context, rec *hc.Record) {
	if z == nil || z.logger == nil || rec == nil {
		return
	}
	checked := z.check(rec.Level(), rec.Message())
	if checked == nil {
		return
	}
	fields := rec.Fields()
	if len(fields) == 0 {
		checked.Write()
		return
	}

	var seen map[string]struct{}
	if len(fields) > 24 {
		seen = make(map[string]struct{}, len(fields)*2)
	}
	zapFields := make([]zap.Field, 0, len(fields))
	for i := range fields {
		f := fields[i]
		if !lastOccurrence(fields, i, seen) {
			continue
		}
		if seen != nil {
			seen[f.Key()] = struct{}{}
		}
		zapFields = append(zapFields, fieldOf(f))
	}
	checked.Write(zapFields...)
}

// fieldOf maps a typed record field to the matching zap constructor.
// Error fields render the message string and raw bytes render via
// zap.Any (base64) — both the v0 adapter's shapes for those payloads.
func fieldOf(f hc.Field) zap.Field {
	if err, ok := f.Err(); ok {
		return zap.String(f.Key(), err.Error())
	}
	if raw, ok := f.Raw(); ok {
		return zap.Any(f.Key(), raw)
	}
	if str, ok := f.Str(); ok {
		return zap.String(f.Key(), str)
	}
	if i, ok := f.Int(); ok {
		return zap.Int64(f.Key(), i)
	}
	if u, ok := f.Uint(); ok {
		return zap.Uint64(f.Key(), u)
	}
	if fl, ok := f.Float(); ok {
		if f.Kind() == hc.KindFloat32 {
			return zap.Float32(f.Key(), float32(fl))
		}
		return zap.Float64(f.Key(), fl)
	}
	if b, ok := f.Bool(); ok {
		return zap.Bool(f.Key(), b)
	}
	if tm, ok := f.Time(); ok {
		return zap.Time(f.Key(), tm)
	}
	if d, ok := f.Duration(); ok {
		return zap.Duration(f.Key(), d)
	}
	if err, ok := f.Err(); ok {
		return zap.String(f.Key(), err.Error())
	}
	return zap.Any(f.Key(), f.Any())
}

func (z *Sink) check(level hc.Level, message string) *zapcore.CheckedEntry {
	switch level {
	case hc.LevelDebug:
		return z.logger.Check(zapcore.DebugLevel, message)
	case hc.LevelWarn:
		return z.logger.Check(zapcore.WarnLevel, message)
	case hc.LevelError:
		return z.logger.Check(zapcore.ErrorLevel, message)
	default:
		return z.logger.Check(zapcore.InfoLevel, message)
	}
}

// lastOccurrence reports whether index i holds the field's last write
// (the encode-side duplicate resolution, mirrored from the core: linear
// scan for narrow events, seen-set for wide ones so the 128-field
// matrix point stays linear).
func lastOccurrence(fields []hc.Field, i int, seen map[string]struct{}) bool {
	if seen != nil {
		_, dup := seen[fields[i].Key()]
		return !dup
	}
	for j := i + 1; j < len(fields); j++ {
		if fields[j].Key() == fields[i].Key() {
			return false
		}
	}
	return true
}

var _ hc.Sink = (*Sink)(nil)
