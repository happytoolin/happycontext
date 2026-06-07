package hc

// Level represents event severity.
type Level string

const (
	// LevelDebug represents debug-level severity.
	LevelDebug Level = "DEBUG"
	// LevelInfo represents info-level severity.
	LevelInfo Level = "INFO"
	// LevelWarn represents warn-level severity.
	LevelWarn Level = "WARN"
	// LevelError represents error-level severity.
	LevelError Level = "ERROR"
)

// Sink receives finalized request events.
type Sink interface {
	Write(level Level, message string, fields map[string]any)
}

// FieldKind identifies the concrete scalar stored in a borrowed field.
type FieldKind uint8

const (
	// FieldAny stores Value.
	FieldAny FieldKind = iota
	// FieldString stores StringValue.
	FieldString
	// FieldInt stores IntValue.
	FieldInt
	// FieldInt64 stores Int64Value.
	FieldInt64
	// FieldBool stores BoolValue.
	FieldBool
)

// BorrowedField is a borrowed internal field view.
//
// Implementations must not retain BorrowedField slices. Use Any when a generic
// boxed value is needed.
type BorrowedField struct {
	Key         string
	Value       any
	StringValue string
	IntValue    int
	Int64Value  int64
	BoolValue   bool
	Kind        FieldKind
}

// Any returns the field value as a generic value.
func (f BorrowedField) Any() any {
	switch f.Kind {
	case FieldString:
		return f.StringValue
	case FieldInt:
		return f.IntValue
	case FieldInt64:
		return f.Int64Value
	case FieldBool:
		return f.BoolValue
	default:
		return f.Value
	}
}

func borrowedAny(key string, value any) BorrowedField {
	return BorrowedField{Key: key, Value: value}
}

func borrowedString(key, value string) BorrowedField {
	return BorrowedField{Key: key, StringValue: value, Kind: FieldString}
}

func borrowedInt(key string, value int) BorrowedField {
	return BorrowedField{Key: key, IntValue: value, Kind: FieldInt}
}

func borrowedInt64(key string, value int64) BorrowedField {
	return BorrowedField{Key: key, Int64Value: value, Kind: FieldInt64}
}

func borrowedBool(key string, value bool) BorrowedField {
	return BorrowedField{Key: key, BoolValue: value, Kind: FieldBool}
}

// borrowedFieldSink receives borrowed typed fields without requiring string/int
// values to be boxed into interface{}.
type borrowedFieldSink interface {
	WriteBorrowedFields(level Level, message string, fields []BorrowedField)
}

// fieldSink receives a borrowed ordered field view.
//
// Implementations must not mutate or retain fields or values that are only
// safe for synchronous use. This fast path avoids building a map for small
// finalized events.
type fieldSink interface {
	WriteFields(level Level, message string, fields []Field)
}

// unsafeMapSink receives a borrowed map view.
//
// Implementations must not mutate or retain fields. This fast path is used
// only when an event has already promoted its internal field store to a map.
type unsafeMapSink interface {
	WriteUnsafe(level Level, message string, fields map[string]any)
}

// completionFieldSink receives borrowed base fields plus derived completion
// fields without requiring the core package to allocate a combined field slice.
type completionFieldSink interface {
	WriteFieldsWithCompletion(level Level, message string, fields []Field, durationMS int64, code int, outcome Outcome)
}

// borrowedCompletionFieldSink receives borrowed typed base fields plus derived
// completion fields without requiring the core package to allocate or box
// scalar values.
type borrowedCompletionFieldSink interface {
	WriteBorrowedFieldsWithCompletion(level Level, message string, fields []BorrowedField, durationMS int64, code int, outcome Outcome)
}

// operationCompletionFieldSink receives borrowed user fields plus operation
// envelope and completion scalars without requiring the core package to
// allocate a combined field slice.
type operationCompletionFieldSink interface {
	WriteFieldsWithOperationCompletion(level Level, message string, fields []Field, start OperationStart, durationMS int64, code int, outcome Outcome)
}

// borrowedOperationCompletionFieldSink receives borrowed typed user fields plus
// operation envelope and completion scalars.
type borrowedOperationCompletionFieldSink interface {
	WriteBorrowedFieldsWithOperationCompletion(level Level, message string, fields []BorrowedField, start OperationStart, durationMS int64, code int, outcome Outcome)
}
