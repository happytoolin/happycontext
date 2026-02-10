package hc

import (
	"fmt"
	"maps"
	"sync"
	"time"
)

// Event accumulates request-scoped structured fields.
type Event struct {
	mu                sync.RWMutex
	fields            map[string]any
	startTime         time.Time
	hasError          bool
	requestedLevel    Level
	hasRequestedLevel bool
}

type snapshot struct {
	fields    map[string]any
	startTime time.Time
	hasError  bool
}

func newEvent() *Event {
	return &Event{
		startTime: time.Now(),
	}
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
	if e.fields == nil {
		pairs := 1 + len(kv)/2
		capHint := 8
		if pairs > capHint {
			capHint = pairs
		}
		e.fields = make(map[string]any, capHint)
	}
	e.fields[key] = value
	for i := 0; i < len(kv); i += 2 {
		e.fields[kv[i].(string)] = kv[i+1]
	}
	return true
}

func (e *Event) setRoute(route string) {
	if route == "" {
		return
	}
	e.mu.Lock()
	if e.fields == nil {
		e.fields = make(map[string]any, 8)
	}
	e.fields["http.route"] = route
	e.mu.Unlock()
}

func (e *Event) setError(err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fields == nil {
		e.fields = make(map[string]any, 8)
	}
	e.hasError = true
	e.fields["error"] = map[string]any{
		"message": err.Error(),
		"type":    fmt.Sprintf("%T", err),
	}
}

func (e *Event) hasErrorValue() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hasError
}

func (e *Event) startedAt() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.startTime
}

func (e *Event) setLevel(level Level) bool {
	if !isValidLevel(level) {
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
		fields:    maps.Clone(e.fields),
		startTime: e.startTime,
		hasError:  e.hasError,
	}
}
