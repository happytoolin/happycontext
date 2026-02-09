package hc

import (
	"context"
	"testing"
)

func TestCommitWritesEventSnapshot(t *testing.T) {
	ctx, _ := NewContext(context.Background())
	Add(ctx, "k", "v")
	sink := NewTestSink()

	if !Commit(ctx, sink, LevelWarn) {
		t.Fatal("expected commit to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != LevelWarn {
		t.Fatalf("level = %s, want %s", events[0].Level, LevelWarn)
	}
	if events[0].Message != defaultMessage {
		t.Fatalf("message = %q, want %q", events[0].Message, defaultMessage)
	}
	if events[0].Fields["k"] != "v" {
		t.Fatalf("field k = %v", events[0].Fields["k"])
	}
}

func TestCommitNoopGuards(t *testing.T) {
	if Commit(context.Background(), nil, LevelInfo) {
		t.Fatal("expected commit with nil sink to return false")
	}

	sink := NewTestSink()
	if Commit(context.Background(), sink, LevelInfo) {
		t.Fatal("expected commit without event to return false")
	}
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected no events, got %d", got)
	}
}
