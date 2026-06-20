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

type fieldKind uint8

const (
	fieldAny fieldKind = iota
	fieldString
	fieldInt
	fieldInt64
	fieldBool
)

type borrowedField struct {
	Key         string
	Value       any
	StringValue string
	IntValue    int
	Int64Value  int64
	BoolValue   bool
	Kind        fieldKind
}

func (f borrowedField) Any() any {
	switch f.Kind {
	case fieldString:
		return f.StringValue
	case fieldInt:
		return f.IntValue
	case fieldInt64:
		return f.Int64Value
	case fieldBool:
		return f.BoolValue
	default:
		return f.Value
	}
}

func borrowedAny(key string, value any) borrowedField {
	return borrowedField{Key: key, Value: value}
}

func borrowedString(key, value string) borrowedField {
	return borrowedField{Key: key, StringValue: value, Kind: fieldString}
}

func borrowedInt(key string, value int) borrowedField {
	return borrowedField{Key: key, IntValue: value, Kind: fieldInt}
}

func borrowedInt64(key string, value int64) borrowedField {
	return borrowedField{Key: key, Int64Value: value, Kind: fieldInt64}
}

func borrowedBool(key string, value bool) borrowedField {
	return borrowedField{Key: key, BoolValue: value, Kind: fieldBool}
}
