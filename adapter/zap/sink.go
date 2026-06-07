package zapadapter

import (
	"sort"
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

var zapKeyPool = sync.Pool{
	New: func() any {
		buf := make([]string, 0, 32)
		return &buf
	},
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
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
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
		*keysPtr = keys[:0]
		zapKeyPool.Put(keysPtr)
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zap.Any(field.Key, field.Value))
	}
	z.write(level, message, zapFields)
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zapFieldFromBorrowed(field))
	}
	z.write(level, message, zapFields)
}

// WriteFieldsWithCompletion writes borrowed base fields plus lifecycle completion fields.
func (z *Sink) WriteFieldsWithCompletion(level hc.Level, message string, fields []hc.Field, durationMS int64, code int, outcome hc.Outcome) {
	if z == nil || z.logger == nil {
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zap.Any(field.Key, field.Value))
	}
	zapFields = append(zapFields,
		zap.Int64("duration_ms", durationMS),
		zap.Int("op.code", code),
		zap.String("op.outcome", string(outcome)),
	)
	z.write(level, message, zapFields)
}

// WriteBorrowedFieldsWithCompletion writes borrowed typed fields plus lifecycle completion fields.
func (z *Sink) WriteBorrowedFieldsWithCompletion(level hc.Level, message string, fields []hc.BorrowedField, durationMS int64, code int, outcome hc.Outcome) {
	if z == nil || z.logger == nil {
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zapFieldFromBorrowed(field))
	}
	zapFields = append(zapFields,
		zap.Int64("duration_ms", durationMS),
		zap.Int("op.code", code),
		zap.String("op.outcome", string(outcome)),
	)
	z.write(level, message, zapFields)
}

// WriteFieldsWithOperationCompletion writes borrowed fields plus operation envelope and completion fields.
func (z *Sink) WriteFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.Field, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	if z == nil || z.logger == nil {
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zap.Any(field.Key, field.Value))
	}
	zapFields = appendOperationFields(zapFields, start)
	zapFields = append(zapFields,
		zap.Int64("duration_ms", durationMS),
		zap.Int("op.code", code),
		zap.String("op.outcome", string(outcome)),
	)
	z.write(level, message, zapFields)
}

// WriteBorrowedFieldsWithOperationCompletion writes borrowed typed fields plus operation envelope and completion fields.
func (z *Sink) WriteBorrowedFieldsWithOperationCompletion(level hc.Level, message string, fields []hc.BorrowedField, start hc.OperationStart, durationMS int64, code int, outcome hc.Outcome) {
	if z == nil || z.logger == nil {
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

	bufPtr := zapFieldPool.Get().(*[]zap.Field)
	zapFields := (*bufPtr)[:0]
	defer func() {
		*bufPtr = zapFields[:0]
		zapFieldPool.Put(bufPtr)
	}()

	for _, field := range fields {
		zapFields = append(zapFields, zapFieldFromBorrowed(field))
	}
	zapFields = appendOperationFields(zapFields, start)
	zapFields = append(zapFields,
		zap.Int64("duration_ms", durationMS),
		zap.Int("op.code", code),
		zap.String("op.outcome", string(outcome)),
	)
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

func appendOperationFields(fields []zap.Field, start hc.OperationStart) []zap.Field {
	fields = append(fields, zap.String("op.domain", string(start.Domain)), zap.String("op.name", start.Name))
	if start.ID != "" {
		fields = append(fields, zap.String("op.id", start.ID))
	}
	if start.Source != "" {
		fields = append(fields, zap.String("op.source", start.Source))
	}
	if start.Attempt > 0 {
		fields = append(fields, zap.Int("op.attempt", start.Attempt))
	}
	if start.MaxAttempts > 0 {
		fields = append(fields, zap.Int("op.max_attempts", start.MaxAttempts))
	}
	return fields
}

func zapFieldFromBorrowed(field hc.BorrowedField) zap.Field {
	switch field.Kind {
	case hc.FieldString:
		return zap.String(field.Key, field.StringValue)
	case hc.FieldInt:
		return zap.Int(field.Key, field.IntValue)
	case hc.FieldInt64:
		return zap.Int64(field.Key, field.Int64Value)
	case hc.FieldBool:
		return zap.Bool(field.Key, field.BoolValue)
	default:
		return zap.Any(field.Key, field.Value)
	}
}

func addOperationFieldsToMap(m map[string]any, start hc.OperationStart) {
	if m == nil {
		return
	}
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
