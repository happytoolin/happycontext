package zapadapter

import (
	"sort"
	"sync"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
)

// ponytail: retain buffers through the tested 100-field case; raise the limit
// only if larger events are common enough to justify the retained memory.
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

var zapKeyPool = sync.Pool{
	New: func() any {
		buf := make([]string, 0, zapPoolCapacity)
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
type SinkOptions struct {
	// DeterministicOrder sorts keys before writing fields.
	DeterministicOrder bool
}

// Sink writes happycontext events to zap.
type Sink struct {
	logger             *zap.Logger
	deterministicOrder bool
}

// New creates a zap-backed sink.
func New(l *zap.Logger) *Sink {
	return NewWithOptions(l, SinkOptions{})
}

// NewWithOptions creates a zap-backed sink with options.
func NewWithOptions(l *zap.Logger, opts SinkOptions) *Sink {
	return &Sink{logger: l, deterministicOrder: opts.DeterministicOrder}
}

// Write implements hc.Sink.
func (z *Sink) Write(level hc.Level, message string, fields map[string]any) {
	if z == nil || z.logger == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		recycleSlice(&zapFieldPool, bufPtr, zapFields)
	}()

	if !z.deterministicOrder {
		for k, v := range fields {
			zapFields = append(zapFields, zap.Any(k, v))
		}
		z.write(level, message, zapFields)
		return
	}

	keysPtr := zapKeyPool.Get().(*[]string)
	keys := (*keysPtr)[:0]
	defer func() {
		recycleSlice(&zapKeyPool, keysPtr, keys)
	}()

	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		zapFields = append(zapFields, zap.Any(k, fields[k]))
	}

	z.write(level, message, zapFields)
}

func (z *Sink) write(level hc.Level, message string, fields []zap.Field) {
	switch level {
	case hc.LevelDebug:
		z.logger.Debug(message, fields...)
	case hc.LevelWarn:
		z.logger.Warn(message, fields...)
	case hc.LevelError:
		z.logger.Error(message, fields...)
	default:
		z.logger.Info(message, fields...)
	}
}

var _ hc.Sink = (*Sink)(nil)
