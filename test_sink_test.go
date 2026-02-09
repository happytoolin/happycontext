package hc

import (
	"testing"
)

func TestTestSinkCopiesInputFields(t *testing.T) {
	sink := NewTestSink()
	fields := map[string]any{"a": 1}
	sink.Write(LevelInfo, "m", fields)
	fields["a"] = 2

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["a"] != 1 {
		t.Fatalf("expected copied field value 1, got %v", events[0].Fields["a"])
	}
}

func TestTestSinkDeepCopiesNestedInputFields(t *testing.T) {
	sink := NewTestSink()
	fields := map[string]any{
		"user": map[string]any{
			"roles": []any{"admin"},
		},
	}

	sink.Write(LevelInfo, "m", fields)

	fields["user"].(map[string]any)["roles"].([]any)[0] = "viewer"

	events := sink.Events()
	user := events[0].Fields["user"].(map[string]any)
	role := user["roles"].([]any)[0]
	if role != "admin" {
		t.Fatalf("expected deep-copied nested role=admin, got %v", role)
	}
}

func TestTestSinkEventsReturnsCopy(t *testing.T) {
	sink := NewTestSink()
	sink.Write(LevelInfo, "m", map[string]any{"a": 1})

	events := sink.Events()
	events[0].Level = LevelError

	fresh := sink.Events()
	if fresh[0].Level != LevelInfo {
		t.Fatalf("expected original level to remain %s, got %s", LevelInfo, fresh[0].Level)
	}
}

func TestTestSinkEventsReturnsDeepCopyOfFields(t *testing.T) {
	sink := NewTestSink()
	sink.Write(LevelInfo, "m", map[string]any{
		"user": map[string]any{
			"id": "u_1",
		},
	})

	events := sink.Events()
	events[0].Fields["user"].(map[string]any)["id"] = "u_2"

	fresh := sink.Events()
	id := fresh[0].Fields["user"].(map[string]any)["id"]
	if id != "u_1" {
		t.Fatalf("expected original nested id to remain u_1, got %v", id)
	}
}
