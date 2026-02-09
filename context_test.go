package hc

import (
	"context"
	"errors"
	"testing"
)

func TestContextHelpersNoopWithoutEvent(t *testing.T) {
	ctx := context.Background()
	Add(ctx, "a", 1)
	AddMap(ctx, map[string]any{"b": 2})
	Error(ctx, errors.New("boom"))
}

func TestContextHelpers(t *testing.T) {
	ctx, _ := NewContext(context.Background())
	Add(ctx, "a", 1)
	Add(ctx, "alias", true)
	AddMap(ctx, map[string]any{"b": 2})
	Error(ctx, errors.New("boom"))
	SetLevel(ctx, LevelWarn)
	SetRoute(ctx, "/orders/:id")

	e := FromContext(ctx)
	if e == nil {
		t.Fatal("event missing in context")
	}
	s := e.Snapshot()
	if s.Fields["a"] != 1 || s.Fields["b"] != 2 {
		t.Fatalf("missing fields: %#v", s.Fields)
	}
	if s.Fields["alias"] != true {
		t.Fatalf("expected alias field")
	}
	if !s.HasError {
		t.Fatal("expected HasError true")
	}
	if s.Fields["http.route"] != "/orders/:id" {
		t.Fatalf("expected explicit route field, got %#v", s.Fields["http.route"])
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
	if Add(nil, "a", 1) {
		t.Fatal("expected Add(nil, ...) to return false")
	}
	if AddMap(nil, map[string]any{"a": 1}) {
		t.Fatal("expected AddMap(nil, ...) to return false")
	}
	if Error(nil, errors.New("boom")) {
		t.Fatal("expected Error(nil, ...) to return false")
	}
	if SetLevel(nil, LevelInfo) {
		t.Fatal("expected SetLevel(nil, ...) to return false")
	}
	if SetRoute(nil, "/x") {
		t.Fatal("expected SetRoute(nil, ...) to return false")
	}
	if got := FromContext(nil); got != nil {
		t.Fatal("expected FromContext(nil) to be nil")
	}
}
