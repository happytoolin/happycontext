package slogadapter

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/happytoolin/happycontext"
)

var slogAttrPool = sync.Pool{
	New: func() any {
		buf := make([]slog.Attr, 0, 32)
		return &buf
	},
}

var slogKeyPool = sync.Pool{
	New: func() any {
		buf := make([]string, 0, 32)
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

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	if !s.deterministicOrder {
		for k, v := range fields {
			attrs = append(attrs, slog.Any(k, v))
		}
		s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
		return
	}
	keysPtr := slogKeyPool.Get().(*[]string)
	keys := (*keysPtr)[:0]
	defer func() {
		*keysPtr = keys[:0]
		slogKeyPool.Put(keysPtr)
	}()
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		attrs = append(attrs, slog.Any(k, fields[k]))
	}
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteUnsafe writes a borrowed field map without retaining it.
func (s *Sink) WriteUnsafe(level hc.Level, message string, fields map[string]any) {
	s.Write(level, message, fields)
}

// WriteFields writes borrowed ordered fields without first building a map.
func (s *Sink) WriteFields(level hc.Level, message string, fields []hc.Field) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		s.Write(level, message, mapFromFields(fields))
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slog.Any(field.Key, field.Value))
	}
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteBorrowedFields writes borrowed typed fields without first building a map.
func (s *Sink) WriteBorrowedFields(level hc.Level, message string, fields []hc.BorrowedField) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		s.Write(level, message, mapFromBorrowedFields(fields))
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slogAttrFromBorrowed(field))
	}
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteFieldsWithCompletion writes borrowed base fields plus lifecycle completion fields.
func (s *Sink) WriteFieldsWithCompletion(level hc.Level, message string, fields []hc.Field, durationMS int64, code int, outcome hc.Outcome) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		m := mapFromFields(fields)
		if m == nil {
			m = make(map[string]any, 3)
		}
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		s.Write(level, message, m)
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slog.Any(field.Key, field.Value))
	}
	attrs = append(attrs,
		slog.Int64("duration_ms", durationMS),
		slog.Int("op.code", code),
		slog.String("op.outcome", string(outcome)),
	)
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteBorrowedFieldsWithCompletion writes borrowed typed fields plus lifecycle completion fields.
func (s *Sink) WriteBorrowedFieldsWithCompletion(level hc.Level, message string, fields []hc.BorrowedField, durationMS int64, code int, outcome hc.Outcome) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		m := mapFromBorrowedFields(fields)
		if m == nil {
			m = make(map[string]any, 3)
		}
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		s.Write(level, message, m)
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slogAttrFromBorrowed(field))
	}
	attrs = append(attrs,
		slog.Int64("duration_ms", durationMS),
		slog.Int("op.code", code),
		slog.String("op.outcome", string(outcome)),
	)
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteFieldsWithOperationCompletion writes borrowed fields plus operation envelope and completion fields.
func (s *Sink) WriteFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.Field, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		m := mapFromFields(fields)
		if m == nil {
			m = make(map[string]any, 9)
		}
		addOperationFieldsToMap(m, start)
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		s.Write(level, message, m)
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slog.Any(field.Key, field.Value))
	}
	attrs = appendOperationFields(attrs, start)
	attrs = append(attrs,
		slog.Int64("duration_ms", durationMS),
		slog.Int("op.code", code),
		slog.String("op.outcome", string(outcome)),
	)
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

// WriteBorrowedFieldsWithOperationCompletion writes borrowed typed fields plus operation envelope and completion fields.
func (s *Sink) WriteBorrowedFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.BorrowedField, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	if s == nil || s.logger == nil {
		return
	}

	if message == "" {
		message = hc.DefaultMessage
	}
	if s.deterministicOrder {
		m := mapFromBorrowedFields(fields)
		if m == nil {
			m = make(map[string]any, 9)
		}
		addOperationFieldsToMap(m, start)
		m["duration_ms"] = durationMS
		m["op.code"] = code
		m["op.outcome"] = string(outcome)
		s.Write(level, message, m)
		return
	}

	levelValue := toSlogLevel(level)
	bufPtr := slogAttrPool.Get().(*[]slog.Attr)
	attrs := (*bufPtr)[:0]
	defer func() {
		*bufPtr = attrs[:0]
		slogAttrPool.Put(bufPtr)
	}()

	for _, field := range fields {
		attrs = append(attrs, slogAttrFromBorrowed(field))
	}
	attrs = appendOperationFields(attrs, start)
	attrs = append(attrs,
		slog.Int64("duration_ms", durationMS),
		slog.Int("op.code", code),
		slog.String("op.outcome", string(outcome)),
	)
	s.logger.LogAttrs(context.Background(), levelValue, message, attrs...)
}

func toSlogLevel(level hc.Level) slog.Level {
	switch level {
	case hc.LevelDebug:
		return slog.LevelDebug
	case hc.LevelWarn:
		return slog.LevelWarn
	case hc.LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func appendOperationFields(attrs []slog.Attr, start hc.OperationStart) []slog.Attr {
	attrs = append(attrs, slog.String("op.domain", string(start.Domain)), slog.String("op.name", start.Name))
	if start.ID != "" {
		attrs = append(attrs, slog.String("op.id", start.ID))
	}
	if start.Source != "" {
		attrs = append(attrs, slog.String("op.source", start.Source))
	}
	if start.Attempt > 0 {
		attrs = append(attrs, slog.Int("op.attempt", start.Attempt))
	}
	if start.MaxAttempts > 0 {
		attrs = append(attrs, slog.Int("op.max_attempts", start.MaxAttempts))
	}
	return attrs
}

func slogAttrFromBorrowed(field hc.BorrowedField) slog.Attr {
	switch field.Kind {
	case hc.FieldString:
		return slog.String(field.Key, field.StringValue)
	case hc.FieldInt:
		return slog.Int(field.Key, field.IntValue)
	case hc.FieldInt64:
		return slog.Int64(field.Key, field.Int64Value)
	case hc.FieldBool:
		return slog.Bool(field.Key, field.BoolValue)
	default:
		return slog.Any(field.Key, field.Value)
	}
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
