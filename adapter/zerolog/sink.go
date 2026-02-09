package zerologadapter

import (
	"context"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

// Sink writes happycontext events to zerolog.
type Sink struct {
	logger *zerolog.Logger
}

// New creates a zerolog-backed sink.
func New(l *zerolog.Logger) *Sink {
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

	event := z.logger.Info()
	switch level {
	case hc.LevelDebug:
		event = z.logger.Debug()
	case hc.LevelWarn:
		event = z.logger.Warn()
	case hc.LevelError:
		event = z.logger.Error()
	}

	for k, v := range fields {
		switch val := v.(type) {
		case string:
			event = event.Str(k, val)
		case int:
			event = event.Int(k, val)
		case int8:
			event = event.Int8(k, val)
		case int16:
			event = event.Int16(k, val)
		case int32:
			event = event.Int32(k, val)
		case int64:
			event = event.Int64(k, val)
		case uint:
			event = event.Uint(k, val)
		case uint8:
			event = event.Uint8(k, val)
		case uint16:
			event = event.Uint16(k, val)
		case uint32:
			event = event.Uint32(k, val)
		case uint64:
			event = event.Uint64(k, val)
		case float32:
			event = event.Float32(k, val)
		case float64:
			event = event.Float64(k, val)
		case bool:
			event = event.Bool(k, val)
		case time.Time:
			event = event.Time(k, val)
		case time.Duration:
			event = event.Dur(k, val)
		case error:
			event = event.Str(k, val.Error())
		default:
			event = event.Interface(k, v)
		}
	}
	event.Msg(message)
}

var _ hc.Sink = (*Sink)(nil)
