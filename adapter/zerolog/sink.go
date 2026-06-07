package zerologadapter

import (
	"sort"
	"sync"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

var zerologKeyPool = sync.Pool{
	New: func() any {
		buf := make([]string, 0, 32)
		return &buf
	},
}

// SinkOptions controls zerolog adapter behavior.
type SinkOptions struct {
	// DeterministicOrder sorts keys before writing fields.
	DeterministicOrder bool
}

// Sink writes happycontext events to zerolog.
type Sink struct {
	logger             *zerolog.Logger
	deterministicOrder bool
}

// New creates a zerolog-backed sink.
func New(l *zerolog.Logger) *Sink {
	return NewWithOptions(l, SinkOptions{})
}

// NewWithOptions creates a zerolog-backed sink with options.
func NewWithOptions(l *zerolog.Logger, opts SinkOptions) *Sink {
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

	event := z.logger.Info()
	switch level {
	case hc.LevelDebug:
		event = z.logger.Debug()
	case hc.LevelWarn:
		event = z.logger.Warn()
	case hc.LevelError:
		event = z.logger.Error()
	}

	if !z.deterministicOrder {
		for k, v := range fields {
			event = appendField(event, k, v)
		}
		event.Msg(message)
		return
	}

	keysPtr := zerologKeyPool.Get().(*[]string)
	keys := (*keysPtr)[:0]
	defer func() {
		*keysPtr = keys[:0]
		zerologKeyPool.Put(keysPtr)
	}()

	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		event = appendField(event, k, fields[k])
	}
	event.Msg(message)
}

// WriteUnsafe writes a borrowed field map without retaining it.
func (z *Sink) WriteUnsafe(level hc.Level, message string, fields map[string]any) {
	z.Write(level, message, fields)
}

// WriteFields writes borrowed ordered fields without first building a map.
func (z *Sink) WriteFields(level hc.Level, message string, fields []hc.Field) {
	if z == nil || z.logger == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		z.Write(level, message, mapFromFields(fields))
		return
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

	for _, field := range fields {
		event = appendField(event, field.Key, field.Value)
	}
	event.Msg(message)
}

// WriteBorrowedFields writes borrowed typed fields without first building a map.
func (z *Sink) WriteBorrowedFields(level hc.Level, message string, fields []hc.BorrowedField) {
	if z == nil || z.logger == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		z.Write(level, message, mapFromBorrowedFields(fields))
		return
	}

	event := z.newEvent(level)
	for _, field := range fields {
		event = appendBorrowedField(event, field)
	}
	event.Msg(message)
}

// WriteFieldsWithCompletion writes borrowed base fields plus lifecycle completion fields.
func (z *Sink) WriteFieldsWithCompletion(level hc.Level, message string, fields []hc.Field, durationMS int64, code int, outcome hc.Outcome) {
	event := z.newEvent(level)
	if event == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		m := mapFromFields(fields)
		if m == nil {
			m = make(map[string]any, 3)
		}
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		z.Write(level, message, m)
		return
	}

	for _, field := range fields {
		event = appendField(event, field.Key, field.Value)
	}
	event.Int64("duration_ms", durationMS).
		Int("op.code", code).
		Str("op.outcome", string(outcome)).
		Msg(message)
}

// WriteBorrowedFieldsWithCompletion writes borrowed typed fields plus lifecycle completion fields.
func (z *Sink) WriteBorrowedFieldsWithCompletion(level hc.Level, message string, fields []hc.BorrowedField, durationMS int64, code int, outcome hc.Outcome) {
	event := z.newEvent(level)
	if event == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		m := mapFromBorrowedFields(fields)
		if m == nil {
			m = make(map[string]any, 3)
		}
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		z.Write(level, message, m)
		return
	}

	for _, field := range fields {
		event = appendBorrowedField(event, field)
	}
	event.Int64("duration_ms", durationMS).
		Int("op.code", code).
		Str("op.outcome", string(outcome)).
		Msg(message)
}

// WriteFieldsWithOperationCompletion writes borrowed fields plus operation envelope and completion fields.
func (z *Sink) WriteFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.Field, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	event := z.newEvent(level)
	if event == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		m := mapFromFields(fields)
		if m == nil {
			m = make(map[string]any, 9)
		}
		addOperationFieldsToMap(m, start)
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		z.Write(level, message, m)
		return
	}

	for _, field := range fields {
		event = appendField(event, field.Key, field.Value)
	}
	event = appendOperationFields(event, start)
	event.Int64("duration_ms", durationMS).
		Int("op.code", code).
		Str("op.outcome", string(outcome)).
		Msg(message)
}

// WriteBorrowedFieldsWithOperationCompletion writes borrowed typed fields plus operation envelope and completion fields.
func (z *Sink) WriteBorrowedFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.BorrowedField, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	event := z.newEvent(level)
	if event == nil {
		return
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	if z.deterministicOrder {
		m := mapFromBorrowedFields(fields)
		if m == nil {
			m = make(map[string]any, 9)
		}
		addOperationFieldsToMap(m, start)
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		z.Write(level, message, m)
		return
	}

	for _, field := range fields {
		event = appendBorrowedField(event, field)
	}
	event = appendOperationFields(event, start)
	event.Int64("duration_ms", durationMS).
		Int("op.code", code).
		Str("op.outcome", string(outcome)).
		Msg(message)
}

func appendOperationFields(event *zerolog.Event, start hc.OperationStart) *zerolog.Event {
	event = event.Str("op.domain", string(start.Domain)).Str("op.name", start.Name)
	if start.ID != "" {
		event = event.Str("op.id", start.ID)
	}
	if start.Source != "" {
		event = event.Str("op.source", start.Source)
	}
	if start.Attempt > 0 {
		event = event.Int("op.attempt", start.Attempt)
	}
	if start.MaxAttempts > 0 {
		event = event.Int("op.max_attempts", start.MaxAttempts)
	}
	return event
}

func addOperationFieldsToMap(m map[string]any, start hc.OperationStart) {
	m["op.domain"] = string(start.Domain)
	m["op.name"] = start.Name
	if start.ID != "" {
		m["op.id"] = start.ID
	}
	if start.Source != "" {
		m["op.source"] = start.Source
	}
	if start.Attempt > 0 {
		m["op.attempt"] = start.Attempt
	}
	if start.MaxAttempts > 0 {
		m["op.max_attempts"] = start.MaxAttempts
	}
}

func (z *Sink) newEvent(level hc.Level) *zerolog.Event {
	if z == nil || z.logger == nil {
		return nil
	}
	switch level {
	case hc.LevelDebug:
		return z.logger.Debug()
	case hc.LevelWarn:
		return z.logger.Warn()
	case hc.LevelError:
		return z.logger.Error()
	default:
		return z.logger.Info()
	}
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

func appendBorrowedField(event *zerolog.Event, field hc.BorrowedField) *zerolog.Event {
	switch field.Kind {
	case hc.FieldString:
		return event.Str(field.Key, field.StringValue)
	case hc.FieldInt:
		return event.Int(field.Key, field.IntValue)
	case hc.FieldInt64:
		return event.Int64(field.Key, field.Int64Value)
	case hc.FieldBool:
		return event.Bool(field.Key, field.BoolValue)
	default:
		return appendField(event, field.Key, field.Value)
	}
}

func mapFromFields(fields []hc.Field) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	m := make(map[string]any, len(fields))
	for _, field := range fields {
		m[field.Key] = field.Value
	}
	return m
}

func mapFromBorrowedFields(fields []hc.BorrowedField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	m := make(map[string]any, len(fields))
	for _, field := range fields {
		m[field.Key] = field.Any()
	}
	return m
}

var _ hc.Sink = (*Sink)(nil)
