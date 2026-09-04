// Package slogadapter bridges happycontext records into log/slog: a
// Sink that forwards each finalized record as typed slog attributes on
// the logger's own level threshold.
package slogadapter

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/happytoolin/happycontext"
)

const (
	slogPoolCapacity    = 32
	slogPoolMaxCapacity = 160
)

var slogAttrPool = sync.Pool{
	New: func() any {
		buf := make([]slog.Attr, 0, slogPoolCapacity)
		return &buf
	},
}

func recycleAttrs(bufPtr *[]slog.Attr, buf []slog.Attr) {
	if cap(buf) > slogPoolMaxCapacity {
		return
	}
	clear(buf)
	*bufPtr = buf[:0]
	slogAttrPool.Put(bufPtr)
}

// Sink writes happycontext records to slog.
type Sink struct {
	logger *slog.Logger
}

// New creates a slog-backed sink.
func New(l *slog.Logger) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink: the record's fields are appended in
// insertion order (last-write-wins duplicates resolved) as typed slog
// attributes.
func (s *Sink) Write(ctx context.Context, rec *hc.Record) {
	if s == nil || s.logger == nil || rec == nil {
		return
	}

	slogLevel := slog.LevelInfo
	switch rec.Level() {
	case hc.LevelDebug:
		slogLevel = slog.LevelDebug
	case hc.LevelWarn:
		slogLevel = slog.LevelWarn
	case hc.LevelError:
		slogLevel = slog.LevelError
	}
	if !s.logger.Enabled(ctx, slogLevel) {
		return
	}

	fields := rec.Fields()
	if len(fields) == 0 {
		s.logger.LogAttrs(ctx, slogLevel, rec.Message())
		return
	}

	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() { recycleAttrs(bufPtr, attrs) }()
	for _, i := range lastOccurrences(fields) {
		attrs = append(attrs, attrOf(fields[i]))
	}
	s.logger.LogAttrs(ctx, slogLevel, rec.Message(), attrs...)
}

// attrOf maps a typed field to the matching slog constructor. Error
// fields render the message string and raw bytes render via slog.Any
// (base64) — both the v0 adapter's shapes.
func attrOf(f hc.Field) slog.Attr {
	if err, ok := f.Err(); ok {
		return slog.String(f.Key(), err.Error())
	}
	if raw, ok := f.Raw(); ok {
		return slog.Any(f.Key(), raw)
	}
	if str, ok := f.Str(); ok {
		return slog.String(f.Key(), str)
	}
	if i, ok := f.Int(); ok {
		return slog.Int64(f.Key(), i)
	}
	if u, ok := f.Uint(); ok {
		return slog.Uint64(f.Key(), u)
	}
	if fl, ok := f.Float(); ok {
		// float32 renders widened through slog on Go >= 1.24 (AnyValue
		// converts to Float64Value; slog has no Float32 constructor) —
		// the same shape the v0 adapter produced. The JSON sink and the
		// zap/zerolog bridges preserve 32-bit precision.
		return slog.Float64(f.Key(), fl)
	}
	if b, ok := f.Bool(); ok {
		return slog.Bool(f.Key(), b)
	}
	if tm, ok := f.Time(); ok {
		return slog.Time(f.Key(), tm)
	}
	if d, ok := f.Duration(); ok {
		return slog.Duration(f.Key(), d)
	}
	return slog.Any(f.Key(), f.Any())
}

// lastOccurrences returns the indices of each key's last write, in
// forward emission order — the same duplicate resolution the core
// encoder applies (linear scan for narrow events, backward seen-set
// collection for wide ones so the 128-field matrix point stays linear
// and last-write-wins holds at every width).
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
	slices.Reverse(kept)
	return kept
}

var _ hc.Sink = (*Sink)(nil)
