package hc

import "time"

// Add records one or more fields directly on e.
func (e *Event) Add(key string, value any, kv ...any) bool {
	if e == nil {
		return false
	}
	if len(kv) == 0 {
		e.mu.Lock()
		e.setFieldLocked(key, value)
		e.mu.Unlock()
		return true
	}
	return e.addKV(key, value, kv...)
}

// AddField records one typed field directly on e.
func (e *Event) AddField(field Field) bool {
	if e == nil {
		return false
	}
	return e.addField(field)
}

// AddFields records typed fields directly on e.
func (e *Event) AddFields(fields ...Field) bool {
	if e == nil {
		return false
	}
	return e.addFields(fields...)
}

// Add2 records two fields directly on e.
func (e *Event) Add2(key1 string, value1 any, key2 string, value2 any) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.set2FieldsLocked(key1, value1, key2, value2)
	e.mu.Unlock()
	return true
}

// AddString records a string field directly on e.
func (e *Event) AddString(key, value string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setStringFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// Add2Strings records two string fields directly on e.
func (e *Event) Add2Strings(key1, value1, key2, value2 string) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.set2StringFieldsLocked(key1, value1, key2, value2)
	e.mu.Unlock()
	return true
}

// AddBool records a bool field directly on e.
func (e *Event) AddBool(key string, value bool) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setBoolFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddInt records an int field directly on e.
func (e *Event) AddInt(key string, value int) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setIntFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddInt64 records an int64 field directly on e.
func (e *Event) AddInt64(key string, value int64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setInt64FieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddFloat64 records a float64 field directly on e.
func (e *Event) AddFloat64(key string, value float64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddDuration records a duration field directly on e.
func (e *Event) AddDuration(key string, value time.Duration) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// AddTime records a time field directly on e.
func (e *Event) AddTime(key string, value time.Time) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	e.setFieldLocked(key, value)
	e.mu.Unlock()
	return true
}

// SetRoute records a route field directly on e.
func (e *Event) SetRoute(route string) bool {
	if e == nil || route == "" {
		return false
	}
	e.mu.Lock()
	e.setStringFieldLocked("http.route", route)
	e.mu.Unlock()
	return true
}

// Error records err directly on e.
func (e *Event) Error(err error) bool {
	if e == nil {
		return false
	}
	e.setError(err)
	return true
}

// SetMessage records a per-event message directly on e.
func (e *Event) SetMessage(msg string) bool {
	if e == nil {
		return false
	}
	e.setMessage(msg)
	return true
}

// SetLevel sets a requested level override directly on e.
func (e *Event) SetLevel(level Level) bool {
	if e == nil {
		return false
	}
	return e.setLevel(level)
}
