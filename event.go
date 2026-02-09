package hlog

import (
	"fmt"
	"maps"
	"reflect"
	"sync"
	"time"
)

// Event accumulates request-scoped structured fields.
type Event struct {
	mu                sync.RWMutex
	fields            map[string]any
	startTime         time.Time
	hasError          bool
	requestedLevel    string
	hasRequestedLevel bool
}

// Snapshot is an immutable copy of event state at commit time.
type Snapshot struct {
	Fields    map[string]any
	StartTime time.Time
	HasError  bool
}

// NewEvent creates a new event with initialized field storage and start time.
func NewEvent() *Event {
	return &Event{
		fields:    make(map[string]any),
		startTime: time.Now(),
	}
}

// Add sets one field on the event.
func (e *Event) Add(key string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fields[key] = value
}

// AddMap merges all fields from m into the event.
func (e *Event) AddMap(m map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	maps.Copy(e.fields, m)
}

func (e *Event) setRoute(route string) {
	if route == "" {
		return
	}
	e.mu.Lock()
	e.fields["http.route"] = route
	e.mu.Unlock()
}

// SetError marks the event as failed and stores a structured error.
func (e *Event) SetError(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hasError = true
	e.fields["error"] = map[string]any{
		"message": err.Error(),
		"type":    fmt.Sprintf("%T", err),
	}
}

// HasError reports whether the event has an attached error.
func (e *Event) HasError() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hasError
}

// StartTime returns the event start time.
func (e *Event) StartTime() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.startTime
}

// SetLevel stores a requested level override if valid.
func (e *Event) SetLevel(level string) {
	if !isValidLevel(level) {
		return
	}
	e.mu.Lock()
	e.requestedLevel = level
	e.hasRequestedLevel = true
	e.mu.Unlock()
}

// RequestedLevel returns the requested level override when present.
func (e *Event) RequestedLevel() (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.requestedLevel, e.hasRequestedLevel
}

// Snapshot returns a deep-copied immutable view of current event data.
func (e *Event) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	fields := make(map[string]any, len(e.fields))
	seen := make(map[visit]reflect.Value)
	for key, value := range e.fields {
		fields[key] = deepCopyAny(value, seen)
	}

	return Snapshot{
		Fields:    fields,
		StartTime: e.startTime,
		HasError:  e.hasError,
	}
}

type visit struct {
	typ reflect.Type
	ptr uintptr
}

func deepCopyAny(value any, seen map[visit]reflect.Value) any {
	if value == nil {
		return nil
	}
	return deepCopyValue(reflect.ValueOf(value), seen).Interface()
}

func deepCopyValue(value reflect.Value, seen map[visit]reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if copied, ok := seen[key]; ok {
			return copied
		}
		copied := reflect.New(value.Type().Elem())
		seen[key] = copied
		copied.Elem().Set(deepCopyValue(value.Elem(), seen))
		return copied
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if copied, ok := seen[key]; ok {
			return copied
		}
		copied := reflect.MakeMapWithSize(value.Type(), value.Len())
		seen[key] = copied
		iter := value.MapRange()
		for iter.Next() {
			copiedKey := deepCopyValue(iter.Key(), seen)
			copiedValue := deepCopyValue(iter.Value(), seen)
			copied.SetMapIndex(copiedKey, copiedValue)
		}
		return copied
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		key := visit{typ: value.Type(), ptr: value.Pointer()}
		if copied, ok := seen[key]; ok {
			return copied
		}
		copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		seen[key] = copied
		for i := range value.Len() {
			copied.Index(i).Set(deepCopyValue(value.Index(i), seen))
		}
		return copied
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			copied.Index(i).Set(deepCopyValue(value.Index(i), seen))
		}
		return copied
	case reflect.Struct:
		// Start from a shallow copy so unexported fields are preserved,
		// then deep-copy settable fields to break shared references.
		copied := reflect.New(value.Type()).Elem()
		copied.Set(value)
		for i := range value.NumField() {
			dst := copied.Field(i)
			if !dst.CanSet() {
				continue
			}
			dst.Set(deepCopyValue(value.Field(i), seen))
		}
		return copied
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return deepCopyValue(value.Elem(), seen)
	default:
		return value
	}
}
