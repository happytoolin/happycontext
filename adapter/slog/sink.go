package slogadapter

import (
	"context"
	"log/slog"
	"sort"
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

var slogKeyPool = sync.Pool{
	New: func() any {
		buf := make([]string, 0, slogPoolCapacity)
		return &buf
	},
}

func recycleSlice[T any](pool *sync.Pool, bufPtr *[]T, buf []T) {
	if cap(buf) > slogPoolMaxCapacity {
		return
	}
	clear(buf)
	*bufPtr = buf[:0]
	pool.Put(bufPtr)
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
	return NewWithOptions(l, SinkOptions{})
}

// NewWithOptions creates a slog-backed sink with options.
func NewWithOptions(l *slog.Logger, opts SinkOptions) *Sink {
	return &Sink{logger: l, deterministicOrder: opts.DeterministicOrder}
}

// Write implements hc.Sink.
func (s *Sink) Write(level hc.Level, message string, fields map[string]any) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}

	slogLevel := slog.LevelInfo
	switch level {
	case hc.LevelDebug:
		slogLevel = slog.LevelDebug
	case hc.LevelWarn:
		slogLevel = slog.LevelWarn
	case hc.LevelError:
		slogLevel = slog.LevelError
	}
	ctx := context.Background()
	if !s.logger.Enabled(ctx, slogLevel) {
		return
	}
	if len(fields) == 0 {
		s.logger.LogAttrs(ctx, slogLevel, message)
		return
	}

	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		recycleSlice(&slogAttrPool, bufPtr, attrs)
	}()

	if !s.deterministicOrder {
		for k, v := range fields {
			attrs = append(attrs, slog.Any(k, v))
		}
		s.logger.LogAttrs(ctx, slogLevel, message, attrs...)
		return
	}
	keysPtr := slogKeyPool.Get().(*[]string)
	keys := (*keysPtr)[:0]
	defer func() {
		recycleSlice(&slogKeyPool, keysPtr, keys)
	}()
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		attrs = append(attrs, slog.Any(k, fields[k]))
	}
	s.logger.LogAttrs(ctx, slogLevel, message, attrs...)
}

var _ hc.Sink = (*Sink)(nil)
