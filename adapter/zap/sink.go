package zapadapter

import (
	"context"
	"sync"

	"github.com/happytoolin/hlog"
	"go.uber.org/zap"
)

var zapFieldPool = sync.Pool{
	New: func() any {
		buf := make([]zap.Field, 0, 32)
		return &buf
	},
}

// Sink writes hlog events to zap.
type Sink struct {
	logger *zap.Logger
}

// New creates a zap-backed sink.
func New(l *zap.Logger) *Sink {
	return &Sink{logger: l}
}

// Write implements hlog.Sink.
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
	case hlog.LevelDebug:
		z.logger.Debug(message, zapFields...)
	case hlog.LevelWarn:
		z.logger.Warn(message, zapFields...)
	case hlog.LevelError:
		z.logger.Error(message, zapFields...)
	default:
		z.logger.Info(message, zapFields...)
	}
}

var _ hlog.Sink = (*Sink)(nil)
