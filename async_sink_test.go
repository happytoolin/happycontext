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
