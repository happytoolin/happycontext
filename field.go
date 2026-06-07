package hc

import (
	"context"
	"time"
)

// Field is one structured event field.
type Field struct {
	Key   string
	Value any
}

// Any returns a field with an arbitrary value.
func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// String returns a string field.
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Bool returns a bool field.
func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

// Int returns an int field.
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Int64 returns an int64 field.
func Int64(key string, value int64) Field {
	return Field{Key: key, Value: value}
}

// Float64 returns a float64 field.
func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

// Duration returns a duration field.
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

// Time returns a time field.
func Time(key string, value time.Time) Field {
	return Field{Key: key, Value: value}
}

// Err returns an error field. Nil errors are recorded as nil.
func Err(key string, err error) Field {
	return Field{Key: key, Value: err}
}

// AddField records one typed field on the event stored in ctx.
func AddField(ctx context.Context, field Field) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	return e.addField(field)
}

// AddFields records typed fields on the event stored in ctx.
func AddFields(ctx context.Context, fields ...Field) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	return e.addFields(fields...)
}

// AddString records a string field on the event stored in ctx.
func AddString(ctx context.Context, key, value string) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setStringFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddStrings records one or more string fields on the event stored in ctx.
//
// Additional key/value strings can be passed via kv:
// AddStrings(ctx, "a", "1", "b", "2", "c", "3").
// kv must have even length.
func AddStrings(ctx context.Context, key, value string, kv ...string) bool {
	if len(kv)%2 != 0 {
		return false
	}
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setStringFieldLocked(key, value)
	for i := 0; i < len(kv); i += 2 {
		e.setStringFieldLocked(kv[i], kv[i+1])
	}
	e.mu.Unlock()
	return true
}

// Add2Strings records two string fields on the event stored in ctx.
func Add2Strings(ctx context.Context, key1, value1, key2, value2 string) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.set2StringFieldsLocked(key1, value1, key2, value2)
	e.mu.Unlock()
	return true
}

// AddBool records a bool field on the event stored in ctx.
func AddBool(ctx context.Context, key string, value bool) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setBoolFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddInt records an int field on the event stored in ctx.
func AddInt(ctx context.Context, key string, value int) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setIntFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddInt64 records an int64 field on the event stored in ctx.
func AddInt64(ctx context.Context, key string, value int64) bool {
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setInt64FieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddFloat64 records a float64 field on the event stored in ctx.
func AddFloat64(ctx context.Context, key string, value float64) bool {
	return AddField(ctx, Float64(key, value))
}

// AddDuration records a duration field on the event stored in ctx.
func AddDuration(ctx context.Context, key string, value time.Duration) bool {
	return AddField(ctx, Duration(key, value))
}

// AddTime records a time field on the event stored in ctx.
func AddTime(ctx context.Context, key string, value time.Time) bool {
	return AddField(ctx, Time(key, value))
}
