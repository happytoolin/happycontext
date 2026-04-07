package hc

import (
	"context"
	"testing"
	"time"
)

func TestEventAccessors(t *testing.T) {
	t.Run("nil event returns zero values", func(t *testing.T) {
		if EventHasError(nil) {
			t.Error("EventHasError(nil) should be false")
		}
		if EventHasMessage(nil) {
			t.Error("EventHasMessage(nil) should be false")
		}
		if EventStartTime(nil) != (time.Time{}) {
			t.Error("EventStartTime(nil) should return zero time")
		}
		if EventFields(nil) != nil {
			t.Error("EventFields(nil) should return nil")
		}
		if EventMessage(nil) != "" {
			t.Error("EventMessage(nil) should return empty string")
		}
	})

	t.Run("event accessors with valid event", func(t *testing.T) {
		ctx := context.Background()
		ctx, event := BeginOperation(ctx, OperationStart{
			Domain: DomainHTTP,
			Name:   "test",
		})

		// Initially, no error or message
		if EventHasError(event) {
			t.Error("fresh event should not have error")
		}
		if EventHasMessage(event) {
			t.Error("fresh event should not have message")
		}

		// Set error
		event.setError(context.Canceled)
		if !EventHasError(event) {
			t.Error("event should have error after setError")
		}

		// Set message
		event.setMessage("test message")
		if !EventHasMessage(event) {
			t.Error("event should have message after setMessage")
		}
		if EventMessage(event) != "test message" {
			t.Errorf("EventMessage = %q, want 'test message'", EventMessage(event))
		}

		// Check start time
		if EventStartTime(event).IsZero() {
			t.Error("EventStartTime should not be zero")
		}

		// Check fields
		fields := EventFields(event)
		if fields == nil {
			t.Error("EventFields should not be nil")
		}

		// Use ctx to avoid unused variable error
		_ = ctx
	})
}

func TestEventFieldsMutation(t *testing.T) {
	ctx := context.Background()
	ctx, event := BeginOperation(ctx, OperationStart{
		Domain: DomainHTTP,
		Name:   "test",
	})

	Add(ctx, "key1", "value1", "key2", 42)

	fields := EventFields(event)
	if fields["key1"] != "value1" {
		t.Errorf("fields[key1] = %v, want 'value1'", fields["key1"])
	}
	if fields["key2"] != 42 {
		t.Errorf("fields[key2] = %v, want 42", fields["key2"])
	}
}

func TestEventFieldsReturnsShallowCopy(t *testing.T) {
	ctx, event := NewContext(context.Background())
	nested := map[string]any{"inner": "value"}
	Add(ctx, "top", "original", "nested", nested)

	fields := EventFields(event)
	fields["top"] = "mutated"
	nestedFromSnapshot := fields["nested"].(map[string]any)
	nestedFromSnapshot["inner"] = "changed"

	freshFields := EventFields(event)
	if freshFields["top"] != "original" {
		t.Fatalf("top-level mutation leaked back into event: got %v", freshFields["top"])
	}
	nestedAgain, ok := freshFields["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested field type = %T, want map[string]any", freshFields["nested"])
	}
	if nestedAgain["inner"] != "changed" {
		t.Fatalf("nested mutation should be shared by reference, got %v", nestedAgain["inner"])
	}
}
