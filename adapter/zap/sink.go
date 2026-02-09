package zapadapter

import (
	"context"
	"sync"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
)

var zapFieldPool = sync.Pool{
	New: func() any {
		buf := make([]zap.Field, 0, 32)
		return &buf
	},
}

// Sink writes happycontext events to zap.
type Sink struct {
	logger *zap.Logger
}

// New creates a zap-backed sink.
func New(l *zap.Logger) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink.
func (z *Sink) Write(_ context.Context, level, message string, fields map[string]any) {
	if z == nil || z.logger == nil {
		return
	}
	if message == "" {
		message = "request_completed"
	}

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for k, v := range fields {
		zapFields = append(zapFields, zap.Any(k, v))
	}

	switch level {
	case hc.LevelDebug:
		z.logger.Debug(message, zapFields...)
	case hc.LevelWarn:
		z.logger.Warn(message, zapFields...)
	case hc.LevelError:
		z.logger.Error(message, zapFields...)
	default:
		z.logger.Info(message, zapFields...)
	}
}

var _ hc.Sink = (*Sink)(nil)
