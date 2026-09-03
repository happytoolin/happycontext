package hc

import (
	"bytes"
	"context"
	"sync"
)

// CapturedEvent is one event captured by TestSink — a retained, copied
// snapshot (the Record view is only valid during Write).
type CapturedEvent struct {
	level   Level
	message string
	fields  []Field
}

// Level returns the captured event's severity.
func (c CapturedEvent) Level() Level { return c.level }

// Message returns the captured event's message.
func (c CapturedEvent) Message() string { return c.message }

// Fields returns the captured fields in insertion order.
func (c CapturedEvent) Fields() []Field { return c.fields }

// Lookup returns the last value written under key.
func (c CapturedEvent) Lookup(key string) (any, bool) {
	for i := len(c.fields) - 1; i >= 0; i-- {
		if f := c.fields[i]; f.key == key {
			return valueOf(f), true
		}
	}
	return nil, false
}

// TestSink captures events in memory for tests, copying retained values
// out of the transient Record.
type TestSink struct {
	mu     sync.Mutex
	events []CapturedEvent
}

// NewTestSink returns an empty in-memory sink.
func NewTestSink() *TestSink {
	return &TestSink{}
}

// Write captures one event, deep-copying the field values that are
// mutable (KindAny/KindRaw payloads); typed scalars are immutable by
// construction.
func (t *TestSink) Write(_ context.Context, rec *Record) {
	if t == nil || rec == nil {
		return
	}
	captured := CapturedEvent{
		level:   rec.Level(),
		message: rec.Message(),
		fields:  copyFields(rec.Fields()),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, captured)
}

// Events returns the captured events.
func (t *TestSink) Events() []CapturedEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]CapturedEvent, len(t.events))
	copy(out, t.events)
	return out
}

// Reset drops all captured events.
func (t *TestSink) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = nil
}

func copyFields(fields []Field) []Field {
	if fields == nil {
		return nil
	}
	out := make([]Field, len(fields))
	for i, f := range fields {
		switch f.kind {
		case KindRaw:
			raw, _ := f.val.([]byte)
			out[i] = Field{key: f.key, kind: f.kind, val: bytes.Clone(raw)}
		case KindAny, KindErr:
			out[i] = Field{key: f.key, kind: f.kind, val: deepCopyValue(f.val)}
		default:
			out[i] = f
		}
	}
	return out
}

var _ Sink = (*TestSink)(nil)
