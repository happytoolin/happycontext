// Package slogadapter bridges happycontext records into log/slog: a
// Sink that forwards each finalized record as typed slog attributes on
// the logger's own level threshold.
package slogadapter

import (
	"context"
	"log/slog"
	"sync"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/bridge"
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
	for _, i := range bridge.LastIndices(fields, hc.Field.Key) {
		attrs = append(attrs, attrOf(fields[i]))
	}
	s.logger.LogAttrs(ctx, slogLevel, rec.Message(), attrs...)
}

// attrOf maps a typed field to the matching slog constructor. Error
// fields render the message string; everything without a typed slot
// goes through slog.Any.
func attrOf(f hc.Field) slog.Attr {
	if err, ok := f.Err(); ok {
		return slog.String(f.Key(), bridge.ErrorMessage(err))
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
		// slog widens float32 (no Float32 constructor) — the v0 adapter's
		// shape; the JSON sink and zap/zerolog bridges keep 32-bit
		// precision.
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

var _ hc.Sink = (*Sink)(nil)
