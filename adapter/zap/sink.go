// Package zapadapter bridges happycontext records into zap: a Sink that
// forwards each finalized record as typed zap fields through the
// logger's CheckedEntry path.
package zapadapter

import (
	"context"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/bridge"
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
func (s *Sink) Write(ctx context.Context, rec *hc.Record) {
	if s == nil || s.logger == nil || rec == nil {
		return
	}
	checked := s.check(rec.Level(), rec.Message())
	if checked == nil {
		return
	}
	fields := rec.Fields()
	if len(fields) == 0 {
		checked.Write()
		return
	}

	zapFields := make([]zap.Field, 0, len(fields))
	for _, i := range bridge.LastIndices(fields, hc.Field.Key) {
		zapFields = append(zapFields, fieldOf(fields[i]))
	}
	checked.Write(zapFields...)
}

// fieldOf maps a typed record field to the matching zap constructor.
// Error fields render the message string; everything without a typed
// slot goes through zap.Any.
func fieldOf(f hc.Field) zap.Field {
	if err, ok := f.Err(); ok {
		return zap.String(f.Key(), bridge.ErrorMessage(err))
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
	return zap.Any(f.Key(), f.Any())
}

func (s *Sink) check(level hc.Level, message string) *zapcore.CheckedEntry {
	switch level {
	case hc.LevelDebug:
		return s.logger.Check(zapcore.DebugLevel, message)
	case hc.LevelWarn:
		return s.logger.Check(zapcore.WarnLevel, message)
	case hc.LevelError:
		return s.logger.Check(zapcore.ErrorLevel, message)
	default:
		return s.logger.Check(zapcore.InfoLevel, message)
	}
}

var _ hc.Sink = (*Sink)(nil)
