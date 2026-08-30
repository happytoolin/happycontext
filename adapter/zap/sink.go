package zapadapter

import (
	"sync"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	zapPoolCapacity    = 32
	zapPoolMaxCapacity = 160
)

var zapFieldPool = sync.Pool{
	New: func() any {
		buf := make([]zap.Field, 0, zapPoolCapacity)
		return &buf
	},
}

func recycleSlice[T any](pool *sync.Pool, bufPtr *[]T, buf []T) {
	if cap(buf) > zapPoolMaxCapacity {
		return
	}
	clear(buf)
	*bufPtr = buf[:0]
	pool.Put(bufPtr)
}

// SinkOptions controls zap adapter behavior.
//
// It is currently empty and reserved for future options. The previous
// DeterministicOrder option was removed: adapters no longer sort fields,
// because the map-based sink contract cannot carry insertion order and
// sorting on top of it only masked that. Deterministic field order
// arrives structurally with the v2 record core.
type SinkOptions struct{}

// Sink writes happycontext events to zap.
type Sink struct {
	logger *zap.Logger
}

// New creates a zap-backed sink.
func New(l *zap.Logger) *Sink {
	return NewWithOptions(l, SinkOptions{})
}

// NewWithOptions creates a zap-backed sink with options.
func NewWithOptions(l *zap.Logger, opts SinkOptions) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink.
func (z *Sink) Write(level hc.Level, message string, fields map[string]any) {
	if z == nil || z.logger == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	checked := z.check(level, message)
	if checked == nil {
		return
	}
	if len(fields) == 0 {
		checked.Write()
		return
	}

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		recycleSlice(&zapFieldPool, bufPtr, zapFields)
	}()

	for k, v := range fields {
		zapFields = append(zapFields, zap.Any(k, v))
	}
	checked.Write(zapFields...)
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

var _ hc.Sink = (*Sink)(nil)
