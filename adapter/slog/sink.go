package slogadapter

import (
	"context"
	"log/slog"
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

	var seen map[string]struct{}
	if len(fields) > 24 {
		seen = make(map[string]struct{}, len(fields)*2)
	}
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() { recycleAttrs(bufPtr, attrs) }()
	for i := range fields {
		f := fields[i]
		if !lastOccurrence(fields, i, seen) {
			continue
		}
		markSeen(fields, i, seen)
		attrs = append(attrs, attrOf(f))
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

func markSeen(fields []hc.Field, i int, seen map[string]struct{}) {
	if seen != nil {
		seen[fields[i].Key()] = struct{}{}
	}
}

var _ hc.Sink = (*Sink)(nil)
