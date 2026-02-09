package hlog

import (
	"context"
	"maps"
	"slices"
	"sync"
)

// CapturedEvent is one event captured by TestSink.
type CapturedEvent struct {
	Level   string
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
func (t *TestSink) Write(_ context.Context, level, message string, fields map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := maps.Clone(fields)
	t.events = append(t.events, CapturedEvent{Level: level, Message: message, Fields: cp})
}

// Events returns a copy of captured events.
func (t *TestSink) Events() []CapturedEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.events)
}
