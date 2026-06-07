package hc

import (
	"sync"
	"testing"
	"time"
)

func TestAsyncSinkFlushAndClose(t *testing.T) {
	sink := NewTestSink()
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 4})

	async.Write(LevelInfo, "one", map[string]any{"n": 1})
	async.Write(LevelWarn, "two", map[string]any{"n": 2})

	if !async.Flush() {
		t.Fatal("Flush returned false")
	}

	events := sink.Events()
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Message != "one" || events[1].Message != "two" {
		t.Fatalf("messages = %q, %q; want one, two", events[0].Message, events[1].Message)
	}

	async.Close()
	async.Write(LevelInfo, "ignored", nil)
	if len(sink.Events()) != 2 {
		t.Fatal("write after close should be ignored")
	}
}

func TestAsyncSinkWriteCopiesFieldMap(t *testing.T) {
	sink := NewTestSink()
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 4})

	fields := map[string]any{"user_id": "u_1"}
	async.Write(LevelInfo, "one", fields)
	fields["user_id"] = "mutated"

	if !async.Flush() {
		t.Fatal("Flush returned false")
	}
	async.Close()

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Fields["user_id"] != "u_1" {
		t.Fatalf("captured value = %v, want u_1", events[0].Fields["user_id"])
	}
}

func TestAsyncSinkWriteFieldsCopiesBorrowedFields(t *testing.T) {
	sink := &fieldCapturingSink{}
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 4})

	fields := []Field{{Key: "user_id", Value: "u_1"}}
	async.WriteFields(LevelInfo, "one", fields)
	fields[0].Value = "mutated"

	if !async.Flush() {
		t.Fatal("Flush returned false")
	}
	async.Close()

	captured := sink.Fields()
	if len(captured) != 1 {
		t.Fatalf("captured fields len = %d, want 1", len(captured))
	}
	if captured[0].Value != "u_1" {
		t.Fatalf("captured value = %v, want u_1", captured[0].Value)
	}
}

func TestAsyncSinkWriteFieldsWithCompletionCopiesBorrowedFields(t *testing.T) {
	sink := &fieldCapturingSink{}
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 4})

	fields := []Field{{Key: "user_id", Value: "u_1"}}
	async.WriteFieldsWithCompletion(LevelInfo, "one", fields, 7, 200, OutcomeSuccess)
	fields[0].Value = "mutated"

	if !async.Flush() {
		t.Fatal("Flush returned false")
	}
	async.Close()

	captured := sink.Fields()
	if len(captured) != 4 {
		t.Fatalf("captured fields len = %d, want 4", len(captured))
	}
	if captured[0].Value != "u_1" {
		t.Fatalf("captured value = %v, want u_1", captured[0].Value)
	}
	if captured[1].Key != "duration_ms" || captured[1].Value != int64(7) {
		t.Fatalf("completion duration = %#v, want duration_ms=7", captured[1])
	}
	if captured[2].Key != "op.code" || captured[2].Value != 200 {
		t.Fatalf("completion code = %#v, want op.code=200", captured[2])
	}
	if captured[3].Key != "op.outcome" || captured[3].Value != string(OutcomeSuccess) {
		t.Fatalf("completion outcome = %#v, want success", captured[3])
	}
}

func TestAsyncSinkWriteFieldsWithCompletionKeepsCompletionWithoutBaseFields(t *testing.T) {
	sink := &fieldCapturingSink{}
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 4})

	async.WriteFieldsWithCompletion(LevelInfo, "one", nil, 7, 200, OutcomeSuccess)

	if !async.Flush() {
		t.Fatal("Flush returned false")
	}
	async.Close()

	captured := sink.Fields()
	if len(captured) != 3 {
		t.Fatalf("captured fields len = %d, want 3", len(captured))
	}
	if captured[0].Key != "duration_ms" || captured[0].Value != int64(7) {
		t.Fatalf("completion duration = %#v, want duration_ms=7", captured[0])
	}
	if captured[1].Key != "op.code" || captured[1].Value != 200 {
		t.Fatalf("completion code = %#v, want op.code=200", captured[1])
	}
	if captured[2].Key != "op.outcome" || captured[2].Value != string(OutcomeSuccess) {
		t.Fatalf("completion outcome = %#v, want success", captured[2])
	}
}

func TestAsyncSinkDropWhenFull(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	sink := &blockingSink{started: started, release: release}
	async := NewAsyncSink(sink, AsyncSinkOptions{Buffer: 1, DropWhenFull: true})

	async.Write(LevelInfo, "first", nil)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first write to block")
	}

	async.Write(LevelInfo, "queued", nil)
	async.Write(LevelInfo, "dropped", nil)

	if async.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", async.Dropped())
	}

	close(release)
	async.Close()
}

type blockingSink struct {
	startOnce sync.Once
	started   chan struct{}
	release   chan struct{}
}

func (b *blockingSink) Write(level Level, message string, fields map[string]any) {
	b.startOnce.Do(func() {
		close(b.started)
	})
	<-b.release
}

type fieldCapturingSink struct {
	mu     sync.Mutex
	fields []Field
}

func (f *fieldCapturingSink) Write(level Level, message string, fields map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fields = f.fields[:0]
	for key, value := range fields {
		f.fields = append(f.fields, Field{Key: key, Value: value})
	}
}

func (f *fieldCapturingSink) WriteFields(level Level, message string, fields []Field) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fields = append(f.fields[:0], fields...)
}

func (f *fieldCapturingSink) WriteFieldsWithCompletion(level Level, message string, fields []Field, durationMS int64, code int, outcome Outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fields = append(f.fields[:0], fields...)
	f.fields = append(f.fields,
		Int64("duration_ms", durationMS),
		Int("op.code", code),
		String("op.outcome", string(outcome)),
	)
}

func (f *fieldCapturingSink) Fields() []Field {
	f.mu.Lock()
	defer f.mu.Unlock()

	cp := make([]Field, len(f.fields))
	copy(cp, f.fields)
	return cp
}
