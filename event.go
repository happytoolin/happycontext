package hc

import (
	"maps"
	"sync"
	"time"
)

// Event accumulates request-scoped structured fields.
type Event struct {
	mu                sync.RWMutex
	message           string
	fieldMap          map[string]any
	fieldList         []BorrowedField
	fieldBuf          [5]BorrowedField
	startTime         time.Time
	startMono         int64
	hasError          bool
	requestedLevel    Level
	hasRequestedLevel bool
	pooled            bool
}

type snapshot struct {
	fields    map[string]any
	startTime time.Time
	hasError  bool
	message   string
	level     Level
	hasLevel  bool
}

type eventState struct {
	startTime time.Time
	startMono int64
	hasError  bool
	message   string
	level     Level
	hasLevel  bool

	method     string
	path       string
	statusCode int
	hasStatus  bool
}

func newEvent() *Event {
	e := &Event{}
	e.reset()
	return e
}

func newLocalEvent() *Event {
	e := &Event{}
	e.resetLocal()
	return e
}

func (e *Event) reset() {
	e.message = ""
	e.fieldMap = nil
	e.fieldList = nil
	e.startTime = time.Now()
	e.startMono = 0
	e.hasError = false
	e.requestedLevel = Level("")
	e.hasRequestedLevel = false
}

func (e *Event) resetPooled() {
	e.message = ""
	e.fieldMap = nil
	e.fieldList = nil
	e.startTime = time.Time{}
	e.startMono = monotonicNow()
	e.hasError = false
	e.requestedLevel = Level("")
	e.hasRequestedLevel = false
}

func (e *Event) resetLocal() {
	e.message = ""
	e.fieldMap = nil
	e.fieldList = nil
	e.startTime = time.Time{}
	e.startMono = 0
	e.hasError = false
	e.requestedLevel = Level("")
	e.hasRequestedLevel = false
}

func (e *Event) addKV(key string, value any, kv ...any) bool {
	if len(kv)%2 != 0 {
		return false
	}
	for i := 0; i < len(kv); i += 2 {
		if _, ok := kv[i].(string); !ok {
			return false
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.setFieldLocked(key, value)
	for i := 0; i < len(kv); i += 2 {
		e.setFieldLocked(kv[i].(string), kv[i+1])
	}
	return true
}

func (e *Event) addField(field Field) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.setFieldLocked(field.Key, field.Value)
	return true
}

func (e *Event) addFields(fields ...Field) bool {
	if len(fields) == 0 {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, field := range fields {
		e.setFieldLocked(field.Key, field.Value)
	}
	return true
}

func (e *Event) setRoute(route string) {
	if route == "" {
		return
	}
	e.mu.Lock()
	e.setStringFieldLocked("http.route", route)
	e.mu.Unlock()
}

func (e *Event) setError(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hasError = true
	e.setFieldLocked("error", structuredErrorField(err))
}

func (e *Event) setMessage(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.message = msg
}

func (e *Event) hasErrorValue() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hasError
}

func (e *Event) hasMessageValue() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.message) > 0
}

func (e *Event) startedAt() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.startTime
}

func (e *Event) setLevel(level Level) bool {
	if !IsValidLevel(level) {
		return false
	}
	e.mu.Lock()
	e.requestedLevel = level
	e.hasRequestedLevel = true
	e.mu.Unlock()
	return true
}

func (e *Event) requestedLevelValue() (Level, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.requestedLevel, e.hasRequestedLevel
}

func (e *Event) snapshot() snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return snapshot{
		fields:    e.fieldsSnapshotLocked(),
		startTime: e.startTime,
		hasError:  e.hasError,
		message:   e.message,
		level:     e.requestedLevel,
		hasLevel:  e.hasRequestedLevel,
	}
}

func (e *Event) baseState() eventState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.baseStateUnsafe()
}

func (e *Event) baseStateUnsafe() eventState {
	return eventState{
		startTime: e.startTime,
		startMono: e.startMono,
		hasError:  e.hasError,
		message:   e.message,
		level:     e.requestedLevel,
		hasLevel:  e.hasRequestedLevel,
	}
}

func (e *Event) hasLifecycleOverridesUnsafe() bool {
	return e.hasError || e.message != "" || e.hasRequestedLevel
}

func (e *Event) state() eventState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	state := eventState{
		startTime: e.startTime,
		startMono: e.startMono,
		hasError:  e.hasError,
		message:   e.message,
		level:     e.requestedLevel,
		hasLevel:  e.hasRequestedLevel,
	}

	if e.fieldCountLocked() == 0 {
		return state
	}

	state.method, _ = e.stringFieldValueLocked("http.method")
	state.path, _ = e.stringFieldValueLocked("http.path")
	if status, ok := e.intFieldValueLocked("http.status"); ok {
		state.statusCode = status
		state.hasStatus = true
	}
	return state
}

func (e *Event) fieldsSnapshot() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.fieldsSnapshotLocked()
}

func (e *Event) fieldsSnapshotWithExtra(extra int) map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.fieldsSnapshotWithExtraLocked(extra)
}

func (e *Event) getMessage() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.message
}

func (e *Event) fieldCountLocked() int {
	if e.fieldMap != nil {
		return len(e.fieldMap)
	}
	return len(e.fieldList)
}

func (e *Event) setFieldLocked(key string, value any) {
	switch typed := value.(type) {
	case string:
		e.setStringFieldLocked(key, typed)
	case int:
		e.setIntFieldLocked(key, typed)
	case int64:
		e.setInt64FieldLocked(key, typed)
	case bool:
		e.setBoolFieldLocked(key, typed)
	default:
		e.setAnyFieldLocked(key, value)
	}
}

func (e *Event) setAnyFieldLocked(key string, value any) {
	if e.fieldMap != nil {
		e.fieldMap[key] = value
		return
	}

	for i := range e.fieldList {
		if e.fieldList[i].Key == key {
			e.fieldList[i] = borrowedAny(key, value)
			return
		}
	}

	if e.fieldList == nil {
		e.fieldList = e.fieldBuf[:0]
	}
	if len(e.fieldList) < cap(e.fieldList) {
		e.fieldList = append(e.fieldList, borrowedAny(key, value))
		return
	}

	e.promoteFieldMapLocked()
	e.fieldMap[key] = value
}

func (e *Event) set2FieldsLocked(key1 string, value1 any, key2 string, value2 any) {
	if key1 == key2 {
		e.setFieldLocked(key1, value2)
		return
	}
	if e.fieldMap == nil && len(e.fieldList) == 0 {
		e.fieldList = e.fieldBuf[:0]
		e.fieldList = append(e.fieldList, borrowedFieldFromAny(key1, value1), borrowedFieldFromAny(key2, value2))
		return
	}
	e.setFieldLocked(key1, value1)
	e.setFieldLocked(key2, value2)
}

func borrowedFieldFromAny(key string, value any) BorrowedField {
	switch typed := value.(type) {
	case string:
		return borrowedString(key, typed)
	case int:
		return borrowedInt(key, typed)
	case int64:
		return borrowedInt64(key, typed)
	case bool:
		return borrowedBool(key, typed)
	default:
		return borrowedAny(key, value)
	}
}

func (e *Event) setStringFieldLocked(key, value string) {
	if e.fieldMap != nil {
		e.fieldMap[key] = value
		return
	}

	for i := range e.fieldList {
		if e.fieldList[i].Key == key {
			e.fieldList[i] = borrowedString(key, value)
			return
		}
	}

	if e.fieldList == nil {
		e.fieldList = e.fieldBuf[:0]
	}
	if len(e.fieldList) < cap(e.fieldList) {
		e.fieldList = append(e.fieldList, borrowedString(key, value))
		return
	}

	e.promoteFieldMapLocked()
	e.fieldMap[key] = value
}

func (e *Event) set2StringFieldsLocked(key1, value1, key2, value2 string) {
	if key1 == key2 {
		e.setStringFieldLocked(key1, value2)
		return
	}
	if e.fieldMap == nil && len(e.fieldList) == 0 {
		e.fieldList = e.fieldBuf[:0]
		e.fieldList = append(e.fieldList, borrowedString(key1, value1), borrowedString(key2, value2))
		return
	}
	e.setStringFieldLocked(key1, value1)
	e.setStringFieldLocked(key2, value2)
}

func (e *Event) setIntFieldLocked(key string, value int) {
	if e.fieldMap != nil {
		e.fieldMap[key] = value
		return
	}

	for i := range e.fieldList {
		if e.fieldList[i].Key == key {
			e.fieldList[i] = borrowedInt(key, value)
			return
		}
	}

	if e.fieldList == nil {
		e.fieldList = e.fieldBuf[:0]
	}
	if len(e.fieldList) < cap(e.fieldList) {
		e.fieldList = append(e.fieldList, borrowedInt(key, value))
		return
	}

	e.promoteFieldMapLocked()
	e.fieldMap[key] = value
}

func (e *Event) setInt64FieldLocked(key string, value int64) {
	if e.fieldMap != nil {
		e.fieldMap[key] = value
		return
	}

	for i := range e.fieldList {
		if e.fieldList[i].Key == key {
			e.fieldList[i] = borrowedInt64(key, value)
			return
		}
	}

	if e.fieldList == nil {
		e.fieldList = e.fieldBuf[:0]
	}
	if len(e.fieldList) < cap(e.fieldList) {
		e.fieldList = append(e.fieldList, borrowedInt64(key, value))
		return
	}

	e.promoteFieldMapLocked()
	e.fieldMap[key] = value
}

func (e *Event) setBoolFieldLocked(key string, value bool) {
	if e.fieldMap != nil {
		e.fieldMap[key] = value
		return
	}

	for i := range e.fieldList {
		if e.fieldList[i].Key == key {
			e.fieldList[i] = borrowedBool(key, value)
			return
		}
	}

	if e.fieldList == nil {
		e.fieldList = e.fieldBuf[:0]
	}
	if len(e.fieldList) < cap(e.fieldList) {
		e.fieldList = append(e.fieldList, borrowedBool(key, value))
		return
	}

	e.promoteFieldMapLocked()
	e.fieldMap[key] = value
}

func (e *Event) promoteFieldMapLocked() {
	e.fieldMap = make(map[string]any, len(e.fieldList)+1)
	for _, field := range e.fieldList {
		e.fieldMap[field.Key] = field.Any()
	}
	e.fieldList = nil
}

func (e *Event) stringFieldValueLocked(key string) (string, bool) {
	if e.fieldMap != nil {
		value, ok := e.fieldMap[key].(string)
		return value, ok
	}
	for i := range e.fieldList {
		field := e.fieldList[i]
		if field.Key != key {
			continue
		}
		if field.Kind == FieldString {
			return field.StringValue, true
		}
		value, ok := field.Value.(string)
		return value, ok
	}
	return "", false
}

func (e *Event) intFieldValueLocked(key string) (int, bool) {
	if e.fieldMap != nil {
		return asInt(e.fieldMap[key])
	}
	for i := range e.fieldList {
		field := e.fieldList[i]
		if field.Key != key {
			continue
		}
		switch field.Kind {
		case FieldInt:
			return field.IntValue, true
		case FieldInt64:
			return int(field.Int64Value), true
		default:
			return asInt(field.Value)
		}
	}
	return 0, false
}

func (e *Event) fieldsSnapshotLocked() map[string]any {
	return e.fieldsSnapshotWithExtraLocked(0)
}

func (e *Event) fieldsSnapshotWithExtraLocked(extra int) map[string]any {
	if extra < 0 {
		extra = 0
	}
	if e.fieldMap != nil {
		if extra == 0 {
			return maps.Clone(e.fieldMap)
		}
		fields := make(map[string]any, len(e.fieldMap)+extra)
		for key, value := range e.fieldMap {
			fields[key] = value
		}
		return fields
	}
	if len(e.fieldList) == 0 {
		return nil
	}
	fields := make(map[string]any, len(e.fieldList)+extra)
	for _, field := range e.fieldList {
		fields[field.Key] = field.Any()
	}
	return fields
}

func (e *Event) withBorrowedFieldList(fn func([]BorrowedField)) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.withBorrowedFieldListUnsafe(fn)
}

func (e *Event) withBorrowedFieldListUnsafe(fn func([]BorrowedField)) bool {
	if e.fieldMap != nil {
		return false
	}
	fn(e.fieldList)
	return true
}

func (e *Event) withFieldList(fn func([]Field)) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.withFieldListUnsafe(fn)
}

func (e *Event) withFieldListUnsafe(fn func([]Field)) bool {
	if e.fieldMap != nil {
		return false
	}
	fields := make([]Field, len(e.fieldList))
	for i, field := range e.fieldList {
		fields[i] = Field{Key: field.Key, Value: field.Any()}
	}
	fn(fields)
	return true
}

func (e *Event) withFieldMap(fn func(map[string]any)) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.fieldMap == nil {
		return false
	}
	fn(e.fieldMap)
	return true
}
