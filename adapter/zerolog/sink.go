package zerologadapter

import (
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

// SinkOptions controls zerolog adapter behavior.
//
// It is currently empty and reserved for future options. The previous
// DeterministicOrder option was removed: adapters no longer sort fields,
// because the map-based sink contract cannot carry insertion order and
// sorting on top of it only masked that. Deterministic field order
// arrives structurally with the v2 record core.
type SinkOptions struct{}

// Sink writes happycontext events to zerolog.
type Sink struct {
	logger *zerolog.Logger
}

// New creates a zerolog-backed sink.
func New(l *zerolog.Logger) *Sink {
	return NewWithOptions(l, SinkOptions{})
}

// NewWithOptions creates a zerolog-backed sink with options.
func NewWithOptions(l *zerolog.Logger, opts SinkOptions) *Sink {
	return &Sink{logger: l}
}

// Write implements hc.Sink.
func (z *Sink) Write(level hc.Level, message string, fields map[string]any) {
	if z == nil || z.logger == nil {
		return
	}

	var event *zerolog.Event
	switch level {
	case hc.LevelDebug:
		event = z.logger.Debug()
	case hc.LevelWarn:
		event = z.logger.Warn()
	case hc.LevelError:
		event = z.logger.Error()
	default:
		event = z.logger.Info()
	}
	if !event.Enabled() {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if len(fields) == 0 {
		event.Msg(message)
		return
	}

	for k, v := range fields {
		event = appendField(event, k, v)
	}
	event.Msg(message)
}

func appendField(event *zerolog.Event, key string, value any) *zerolog.Event {
	switch val := value.(type) {
	case string:
		return event.Str(key, val)
	case int:
		return event.Int(key, val)
	case int8:
		return event.Int8(key, val)
	case int16:
		return event.Int16(key, val)
	case int32:
		return event.Int32(key, val)
	case int64:
		return event.Int64(key, val)
	case uint:
		return event.Uint(key, val)
	case uint8:
		return event.Uint8(key, val)
	case uint16:
		return event.Uint16(key, val)
	case uint32:
		return event.Uint32(key, val)
	case uint64:
		return event.Uint64(key, val)
	case float32:
		return event.Float32(key, val)
	case float64:
		return event.Float64(key, val)
	case bool:
		return event.Bool(key, val)
	case time.Time:
		return event.Time(key, val)
	case time.Duration:
		return event.Dur(key, val)
	case error:
		return event.Str(key, val.Error())
	default:
		return event.Interface(key, value)
	}
}

var _ hc.Sink = (*Sink)(nil)
