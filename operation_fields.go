package hc

import "time"

// Add records one or more fields on op's event.
func (op *Operation) Add(key string, value any, kv ...any) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		if len(kv)%2 != 0 {
			return false
		}
		for i := 0; i < len(kv); i += 2 {
			if _, ok := kv[i].(string); !ok {
				return false
			}
		}
		op.event.setFieldLocked(key, value)
		for i := 0; i < len(kv); i += 2 {
			op.event.setFieldLocked(kv[i].(string), kv[i+1])
		}
		return true
	}
	return op.event.addKV(key, value, kv...)
}

// Add2 records two fields on op's event.
func (op *Operation) Add2(key1 string, value1 any, key2 string, value2 any) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.set2FieldsLocked(key1, value1, key2, value2)
		return true
	}
	op.event.mu.Lock()
	op.event.setFieldLocked(key1, value1)
	op.event.setFieldLocked(key2, value2)
	op.event.mu.Unlock()
	return true
}

// AddField records one typed field on op's event.
func (op *Operation) AddField(field Field) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setFieldLocked(field.Key, field.Value)
		return true
	}
	return op.event.addField(field)
}

// AddFields records typed fields on op's event.
func (op *Operation) AddFields(fields ...Field) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		if len(fields) == 0 {
			return false
		}
		for _, field := range fields {
			op.event.setFieldLocked(field.Key, field.Value)
		}
		return true
	}
	return op.event.addFields(fields...)
}

// AddString records a string field on op's event.
func (op *Operation) AddString(key, value string) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setStringFieldLocked(key, value)
		return true
	}
	op.event.mu.Lock()
	op.event.setStringFieldLocked(key, value)
	op.event.mu.Unlock()
	return true
}

// AddStrings records one or more string fields on op's event.
func (op *Operation) AddStrings(key, value string, kv ...string) bool {
	if op == nil || op.event == nil || len(kv)%2 != 0 {
		return false
	}
	if op.unsafeEvent {
		op.event.setStringFieldLocked(key, value)
		for i := 0; i < len(kv); i += 2 {
			op.event.setStringFieldLocked(kv[i], kv[i+1])
		}
		return true
	}
	op.event.mu.Lock()
	op.event.setStringFieldLocked(key, value)
	for i := 0; i < len(kv); i += 2 {
		op.event.setStringFieldLocked(kv[i], kv[i+1])
	}
	op.event.mu.Unlock()
	return true
}

// Add2Strings records two string fields on op's event.
func (op *Operation) Add2Strings(key1, value1, key2, value2 string) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setStringFieldLocked(key1, value1)
		op.event.setStringFieldLocked(key2, value2)
		return true
	}
	op.event.mu.Lock()
	op.event.setStringFieldLocked(key1, value1)
	op.event.setStringFieldLocked(key2, value2)
	op.event.mu.Unlock()
	return true
}

// AddBool records a bool field on op's event.
func (op *Operation) AddBool(key string, value bool) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setBoolFieldLocked(key, value)
		return true
	}
	op.event.mu.Lock()
	op.event.setBoolFieldLocked(key, value)
	op.event.mu.Unlock()
	return true
}

// AddInt records an int field on op's event.
func (op *Operation) AddInt(key string, value int) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setIntFieldLocked(key, value)
		return true
	}
	op.event.mu.Lock()
	op.event.setIntFieldLocked(key, value)
	op.event.mu.Unlock()
	return true
}

// AddInt64 records an int64 field on op's event.
func (op *Operation) AddInt64(key string, value int64) bool {
	if op == nil || op.event == nil {
		return false
	}
	if op.unsafeEvent {
		op.event.setInt64FieldLocked(key, value)
		return true
	}
	op.event.mu.Lock()
	op.event.setInt64FieldLocked(key, value)
	op.event.mu.Unlock()
	return true
}

// AddFloat64 records a float64 field on op's event.
func (op *Operation) AddFloat64(key string, value float64) bool {
	return op.AddField(Float64(key, value))
}

// AddDuration records a duration field on op's event.
func (op *Operation) AddDuration(key string, value time.Duration) bool {
	return op.AddField(Duration(key, value))
}

// AddTime records a time field on op's event.
func (op *Operation) AddTime(key string, value time.Time) bool {
	return op.AddField(Time(key, value))
}

// Error records err on op's event.
func (op *Operation) Error(err error) bool {
	if op == nil || op.event == nil {
		return false
	}
	op.event.setError(err)
	return true
}

// SetMessage records a per-event message on op's event.
func (op *Operation) SetMessage(message string) bool {
	if op == nil || op.event == nil {
		return false
	}
	op.event.setMessage(message)
	return true
}

// SetLevel sets a requested level override on op's event.
func (op *Operation) SetLevel(level Level) bool {
	if op == nil || op.event == nil {
		return false
	}
	return op.event.setLevel(level)
}
