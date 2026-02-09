package hc

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
	tracker := &cycleTracker{}
	for key, value := range e.fields {
		fields[key] = deepCopyAny(value, tracker)
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

type cycleTracker struct {
	fast []fastCopyEntry
	seen map[visit]reflect.Value
}

type fastCopyKind uint8

const (
	fastCopyMap fastCopyKind = iota + 1
	fastCopySlice
)

type fastCopyEntry struct {
	ptr  uintptr
	kind fastCopyKind
	val  any
}

func (t *cycleTracker) lookupFast(ptr uintptr, kind fastCopyKind) (any, bool) {
	if ptr == 0 {
		return nil, false
	}
	for i := range t.fast {
		if t.fast[i].ptr == ptr && t.fast[i].kind == kind {
			return t.fast[i].val, true
		}
	}
	return nil, false
}

func (t *cycleTracker) rememberFast(ptr uintptr, kind fastCopyKind, copied any) {
	if ptr == 0 {
		return
	}
	t.fast = append(t.fast, fastCopyEntry{ptr: ptr, kind: kind, val: copied})
}

func (t *cycleTracker) lookupGeneric(typ reflect.Type, ptr uintptr) (reflect.Value, bool) {
	if ptr == 0 || t.seen == nil {
		return reflect.Value{}, false
	}
	v, ok := t.seen[visit{typ: typ, ptr: ptr}]
	return v, ok
}

func (t *cycleTracker) rememberGeneric(typ reflect.Type, ptr uintptr, copied reflect.Value) {
	if ptr == 0 {
		return
	}
	if t.seen == nil {
		t.seen = make(map[visit]reflect.Value)
	}
	t.seen[visit{typ: typ, ptr: ptr}] = copied
}

func deepCopyAny(value any, tracker *cycleTracker) any {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]any:
		return deepCopyMapStringAny(v, tracker)
	case []any:
		return deepCopySliceAny(v, tracker)
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64,
		complex64, complex128,
		time.Time, time.Duration:
		return v
	default:
		return deepCopyValue(reflect.ValueOf(value), tracker).Interface()
	}
}

func deepCopyMapStringAny(src map[string]any, tracker *cycleTracker) map[string]any {
	if src == nil {
		return nil
	}

	ptr := reflect.ValueOf(src).Pointer()
	if copied, ok := tracker.lookupFast(ptr, fastCopyMap); ok {
		if m, ok := copied.(map[string]any); ok {
			return m
		}
	}

	dst := make(map[string]any, len(src))
	tracker.rememberFast(ptr, fastCopyMap, dst)
	for k, v := range src {
		dst[k] = deepCopyAny(v, tracker)
	}
	return dst
}

func deepCopySliceAny(src []any, tracker *cycleTracker) []any {
	if src == nil {
		return nil
	}

	ptr := reflect.ValueOf(src).Pointer()
	if copied, ok := tracker.lookupFast(ptr, fastCopySlice); ok {
		if s, ok := copied.([]any); ok {
			return s
		}
	}

	dst := make([]any, len(src))
	tracker.rememberFast(ptr, fastCopySlice, dst)
	for i := range src {
		dst[i] = deepCopyAny(src[i], tracker)
	}
	return dst
}

func deepCopyValue(value reflect.Value, tracker *cycleTracker) reflect.Value {
	if !value.IsValid() {
		return value
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		if copied, ok := tracker.lookupGeneric(value.Type(), value.Pointer()); ok {
			return copied
		}
		copied := reflect.New(value.Type().Elem())
		tracker.rememberGeneric(value.Type(), value.Pointer(), copied)
		copied.Elem().Set(deepCopyValue(value.Elem(), tracker))
		return copied
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		if copied, ok := tracker.lookupGeneric(value.Type(), value.Pointer()); ok {
			return copied
		}
		copied := reflect.MakeMapWithSize(value.Type(), value.Len())
		tracker.rememberGeneric(value.Type(), value.Pointer(), copied)
		iter := value.MapRange()
		for iter.Next() {
			copiedKey := deepCopyValue(iter.Key(), tracker)
			copiedValue := deepCopyValue(iter.Value(), tracker)
			copied.SetMapIndex(copiedKey, copiedValue)
		}
		return copied
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		if copied, ok := tracker.lookupGeneric(value.Type(), value.Pointer()); ok {
			return copied
		}
		copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		tracker.rememberGeneric(value.Type(), value.Pointer(), copied)
		for i := range value.Len() {
			copied.Index(i).Set(deepCopyValue(value.Index(i), tracker))
		}
		return copied
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			copied.Index(i).Set(deepCopyValue(value.Index(i), tracker))
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
			dst.Set(deepCopyValue(value.Field(i), tracker))
		}
		return copied
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		return deepCopyValue(value.Elem(), tracker)
	default:
		return value
	}
}
