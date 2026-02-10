package hc

import (
	"reflect"
	"sync"
	"time"
)

// CapturedEvent is one event captured by TestSink.
type CapturedEvent struct {
	Level   Level
	Message string
	Fields  map[string]any
}

// TestSink captures events in memory for tests.
type TestSink struct {
	mu     sync.Mutex
	events []CapturedEvent
}

// NewTestSink returns an empty in-memory sink.
func NewTestSink() *TestSink {
	return &TestSink{}
}

// Write appends one captured event.
func (t *TestSink) Write(level Level, message string, fields map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := deepCopyFields(fields)
	t.events = append(t.events, CapturedEvent{Level: level, Message: message, Fields: cp})
}

// Events returns a copy of captured events.
func (t *TestSink) Events() []CapturedEvent {
	t.mu.Lock()
	defer t.mu.Unlock()

	cp := make([]CapturedEvent, len(t.events))
	for i := range t.events {
		ev := t.events[i]
		cp[i] = CapturedEvent{
			Level:   ev.Level,
			Message: ev.Message,
			Fields:  deepCopyFields(ev.Fields),
		}
	}
	return cp
}

func deepCopyFields(fields map[string]any) map[string]any {
	tracker := &deepCopyTracker{}
	return deepCopyMapStringAny(fields, tracker)
}

type deepCopyVisit struct {
	typ reflect.Type
	ptr uintptr
}

type deepCopyTracker struct {
	fast []deepCopyFastEntry
	seen map[deepCopyVisit]reflect.Value
}

type deepCopyFastKind uint8

const (
	deepCopyFastMap deepCopyFastKind = iota + 1
	deepCopyFastSlice
)

type deepCopyFastEntry struct {
	ptr  uintptr
	kind deepCopyFastKind
	val  any
}

func (t *deepCopyTracker) lookupFast(ptr uintptr, kind deepCopyFastKind) (any, bool) {
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

func (t *deepCopyTracker) rememberFast(ptr uintptr, kind deepCopyFastKind, copied any) {
	if ptr == 0 {
		return
	}
	t.fast = append(t.fast, deepCopyFastEntry{ptr: ptr, kind: kind, val: copied})
}

func (t *deepCopyTracker) lookupGeneric(typ reflect.Type, ptr uintptr) (reflect.Value, bool) {
	if ptr == 0 || t.seen == nil {
		return reflect.Value{}, false
	}
	v, ok := t.seen[deepCopyVisit{typ: typ, ptr: ptr}]
	return v, ok
}

func (t *deepCopyTracker) rememberGeneric(typ reflect.Type, ptr uintptr, copied reflect.Value) {
	if ptr == 0 {
		return
	}
	if t.seen == nil {
		t.seen = make(map[deepCopyVisit]reflect.Value)
	}
	t.seen[deepCopyVisit{typ: typ, ptr: ptr}] = copied
}

func deepCopyAny(value any, tracker *deepCopyTracker) any {
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

func deepCopyMapStringAny(src map[string]any, tracker *deepCopyTracker) map[string]any {
	if src == nil {
		return nil
	}

	ptr := reflect.ValueOf(src).Pointer()
	if copied, ok := tracker.lookupFast(ptr, deepCopyFastMap); ok {
		if m, ok := copied.(map[string]any); ok {
			return m
		}
	}

	dst := make(map[string]any, len(src))
	tracker.rememberFast(ptr, deepCopyFastMap, dst)
	for k, v := range src {
		dst[k] = deepCopyAny(v, tracker)
	}
	return dst
}

func deepCopySliceAny(src []any, tracker *deepCopyTracker) []any {
	if src == nil {
		return nil
	}

	ptr := reflect.ValueOf(src).Pointer()
	if copied, ok := tracker.lookupFast(ptr, deepCopyFastSlice); ok {
		if s, ok := copied.([]any); ok {
			return s
		}
	}

	dst := make([]any, len(src))
	tracker.rememberFast(ptr, deepCopyFastSlice, dst)
	for i := range src {
		dst[i] = deepCopyAny(src[i], tracker)
	}
	return dst
}

func deepCopyValue(value reflect.Value, tracker *deepCopyTracker) reflect.Value {
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
