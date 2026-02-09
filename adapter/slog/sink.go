package slogadapter

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/happytoolin/happycontext"
)

var slogAnyPool = sync.Pool{
	New: func() any {
		buf := make([]any, 0, 32)
		return &buf
	},
}

// SinkOptions controls slog adapter behavior.
type SinkOptions struct {
	// DeterministicOrder sorts keys before writing attributes.
	DeterministicOrder bool
}

// Sink writes happycontext events to slog.
type Sink struct {
	logger             *slog.Logger
	deterministicOrder bool
}

// New creates a slog-backed sink with default options.
func New(l *slog.Logger) *Sink {
	return &Sink{logger: l}
}

// NewWithOptions creates a slog-backed sink with options.
func NewWithOptions(l *slog.Logger, opts SinkOptions) *Sink {
	return &Sink{logger: l, deterministicOrder: opts.DeterministicOrder}
}

// Write implements happycontext.Sink.
func (s *Sink) Write(ctx context.Context, level, message string, fields map[string]any) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = "request_completed"
	}

	slogLevel := slog.LevelInfo
	switch level {
	case happycontext.LevelDebug:
		slogLevel = slog.LevelDebug
	case happycontext.LevelWarn:
		slogLevel = slog.LevelWarn
	case happycontext.LevelError:
		slogLevel = slog.LevelError
	}

	bufPtr := slogAnyPool.Get().(*[]any)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAnyPool.Put(bufPtr)
	}()

	if !s.deterministicOrder {
		for k, v := range fields {
			attrs = append(attrs, slog.Any(k, v))
		}
		s.logger.Log(ctx, slogLevel, message, attrs...)
		return
	}
	keys := slices.Collect(maps.Keys(fields))
	slices.Sort(keys)

	for _, k := range keys {
		attrs = append(attrs, slog.Any(k, fields[k]))
	}
	s.logger.Log(ctx, slogLevel, message, attrs...)
}

var _ happycontext.Sink = (*Sink)(nil)
