package hc

import (
	"sync"
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
	tracker := &cycleTracker{}
	return deepCopyMapStringAny(fields, tracker)
}
