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

	zapFields := make([]zap.Field, 0, len(fields))
	for _, i := range lastOccurrences(fields) {
		zapFields = append(zapFields, fieldOf(fields[i]))
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
