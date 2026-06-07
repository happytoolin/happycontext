package hc

import (
	"context"
	"testing"
	"time"
)

func TestAddFieldsRecordsTypedFields(t *testing.T) {
	ctx, event := NewContext(context.Background())
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)

	if !AddFields(ctx,
		String("user_id", "u_1"),
		Int("attempt", 2),
		Bool("cached", true),
		Duration("latency", 5*time.Millisecond),
		Time("created_at", now),
	) {
		t.Fatal("AddFields returned false")
	}

	fields := EventFields(event)
	if fields["user_id"] != "u_1" {
		t.Fatalf("user_id = %v, want u_1", fields["user_id"])
	}
	if fields["attempt"] != 2 {
		t.Fatalf("attempt = %v, want 2", fields["attempt"])
	}
	if fields["cached"] != true {
		t.Fatalf("cached = %v, want true", fields["cached"])
	}
	if fields["latency"] != 5*time.Millisecond {
		t.Fatalf("latency = %v, want 5ms", fields["latency"])
	}
	if fields["created_at"] != now {
		t.Fatalf("created_at = %v, want %v", fields["created_at"], now)
	}
}

func TestAddFieldsReturnsFalseWithoutEvent(t *testing.T) {
	if AddFields(context.Background(), String("k", "v")) {
		t.Fatal("AddFields returned true without an event")
	}
	if AddString(context.Background(), "k", "v") {
		t.Fatal("AddString returned true without an event")
	}
}

func TestAddFieldsReturnsFalseForEmptyFieldList(t *testing.T) {
	ctx, _ := NewContext(context.Background())
	if AddFields(ctx) {
		t.Fatal("AddFields returned true for an empty field list")
	}
}
