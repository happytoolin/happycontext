package zerologadapter

import (
	"context"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

// Sink writes happycontext records to zerolog.
type Sink struct {
	logger *zerolog.Logger
}

// New creates a zerolog-backed sink.
func New(l *zerolog.Logger) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink: the record's fields are appended in
// insertion order (last-write-wins duplicates resolved) through
// zerolog's typed constructors — the same field shapes the v0 adapter
// produced. Pre-encoded raw JSON is appended verbatim (RawJSON).
func (z *Sink) Write(ctx context.Context, rec *hc.Record) {
	if z == nil || z.logger == nil || rec == nil {
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
	event.Time("time", time.Now())
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
		out := make([]int, 0, len(fields))
		for i := range fields {
			last := true
			for j := i + 1; j < len(fields); j++ {
				if fields[j].Key() == fields[i].Key() {
					last = false
					break
				}
			}
			if last {
				out = append(out, i)
			}
		}
		return out
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
