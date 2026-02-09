package hc

import (
	"context"
	"testing"
)

func TestTestSinkCopiesInputFields(t *testing.T) {
	sink := NewTestSink()
	fields := map[string]any{"a": 1}
	sink.Write(context.Background(), LevelInfo, "m", fields)
	fields["a"] = 2

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["a"] != 1 {
		t.Fatalf("expected copied field value 1, got %v", events[0].Fields["a"])
	}
}

func TestTestSinkEventsReturnsCopy(t *testing.T) {
	sink := NewTestSink()
	sink.Write(context.Background(), LevelInfo, "m", map[string]any{"a": 1})

	events := sink.Events()
	events[0].Level = LevelError

	fresh := sink.Events()
	if fresh[0].Level != LevelInfo {
		t.Fatalf("expected original level to remain %s, got %s", LevelInfo, fresh[0].Level)
	}
}
