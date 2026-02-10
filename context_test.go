package hc

import (
	"context"
	"errors"
	"testing"
)

func TestContextHelpersNoopWithoutEvent(t *testing.T) {
	ctx := context.Background()
	Add(ctx, "a", 1)
	Add(ctx, "b", 2, "c", 3)
	Error(ctx, errors.New("boom"))
}

func TestContextHelpers(t *testing.T) {
	ctx, _ := NewContext(context.Background())
	Add(ctx, "a", 1)
	Add(ctx, "alias", true)
	Add(ctx, "b", 2, "c", 3)
	Error(ctx, errors.New("boom"))
	SetLevel(ctx, LevelWarn)
	SetRoute(ctx, "/orders/:id")

	e := FromContext(ctx)
	if e == nil {
		t.Fatal("event missing in context")
	}
	fields := EventFields(e)
	if fields["a"] != 1 || fields["b"] != 2 || fields["c"] != 3 {
		t.Fatalf("missing fields: %#v", fields)
	}
	if fields["alias"] != true {
		t.Fatalf("expected alias field")
	}
	if !EventHasError(e) {
		t.Fatal("expected HasError true")
	}
	if fields["http.route"] != "/orders/:id" {
		t.Fatalf("expected explicit route field, got %#v", fields["http.route"])
	}
	lvl, ok := GetLevel(ctx)
	if !ok || lvl != LevelWarn {
		t.Fatalf("expected level override %q, got %q (ok=%v)", LevelWarn, lvl, ok)
	}
}

func TestGetLevelWithoutEvent(t *testing.T) {
	lvl, ok := GetLevel(context.Background())
	if ok || lvl != Level("") {
		t.Fatalf("expected empty/no level, got %q (ok=%v)", lvl, ok)
	}
}

func TestHelpersWithNilContextAreNoop(t *testing.T) {
	if Add(context.TODO(), "a", 1) {
		t.Fatal("expected Add(no-event-ctx, ...) to return false")
	}
	if Add(context.TODO(), "a", 1, "b") {
		t.Fatal("expected Add with odd kv length to return false")
	}
	if Add(context.TODO(), "a", 1, "b", 2) {
		t.Fatal("expected Add(no-event-ctx, ...) with kv pairs to return false")
	}
	if Error(context.TODO(), errors.New("boom")) {
		t.Fatal("expected Error(no-event-ctx, ...) to return false")
	}
	if SetLevel(context.TODO(), LevelInfo) {
		t.Fatal("expected SetLevel(no-event-ctx, ...) to return false")
	}
	if SetRoute(context.TODO(), "/x") {
		t.Fatal("expected SetRoute(no-event-ctx, ...) to return false")
	}
	if got := FromContext(context.TODO()); got != nil {
		t.Fatal("expected FromContext(no-event-ctx) to be nil")
	}
}

func TestAddRejectsInvalidPairs(t *testing.T) {
	ctx, _ := NewContext(context.Background())

	if Add(ctx, "a", 1, "b") {
		t.Fatal("expected odd kv length to be rejected")
	}
	if Add(ctx, "a", 1, 2, "b") {
		t.Fatal("expected non-string key to be rejected")
	}

	fields := EventFields(FromContext(ctx))
	if len(fields) != 0 {
		t.Fatalf("expected no writes on invalid input, got %#v", fields)
	}
}
