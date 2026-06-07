package hc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEventDirectFieldMethods(t *testing.T) {
	_, event := NewContext(context.Background())

	if !event.AddString("tenant", "enterprise") {
		t.Fatal("AddString returned false")
	}
	if !event.Add2Strings("http.method", "GET", "http.path", "/orders/1") {
		t.Fatal("Add2Strings returned false")
	}
	if !event.AddInt("http.status", 201) {
		t.Fatal("AddInt returned false")
	}
	if !event.SetRoute("/orders/:id") {
		t.Fatal("SetRoute returned false")
	}
	if !event.Add2("attempt", 2, "cached", true) {
		t.Fatal("Add2 returned false")
	}
	if !event.AddFields(
		Int64("row_count", 99),
		Float64("score", 0.75),
		Bool("fresh", true),
	) {
		t.Fatal("AddFields returned false")
	}

	now := time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)
	if !event.AddDuration("latency", 5*time.Millisecond) {
		t.Fatal("AddDuration returned false")
	}
	if !event.AddTime("created_at", now) {
		t.Fatal("AddTime returned false")
	}

	fields := EventFields(event)
	if fields["tenant"] != "enterprise" {
		t.Fatalf("tenant = %v, want enterprise", fields["tenant"])
	}
	if fields["http.method"] != "GET" {
		t.Fatalf("http.method = %v, want GET", fields["http.method"])
	}
	if fields["http.path"] != "/orders/1" {
		t.Fatalf("http.path = %v, want /orders/1", fields["http.path"])
	}
	if fields["http.status"] != 201 {
		t.Fatalf("http.status = %v, want 201", fields["http.status"])
	}
	if fields["http.route"] != "/orders/:id" {
		t.Fatalf("http.route = %v, want /orders/:id", fields["http.route"])
	}
	if fields["attempt"] != 2 {
		t.Fatalf("attempt = %v, want 2", fields["attempt"])
	}
	if fields["cached"] != true {
		t.Fatalf("cached = %v, want true", fields["cached"])
	}
	if fields["row_count"] != int64(99) {
		t.Fatalf("row_count = %v, want 99", fields["row_count"])
	}
	if fields["score"] != 0.75 {
		t.Fatalf("score = %v, want 0.75", fields["score"])
	}
	if fields["fresh"] != true {
		t.Fatalf("fresh = %v, want true", fields["fresh"])
	}
	if fields["latency"] != 5*time.Millisecond {
		t.Fatalf("latency = %v, want 5ms", fields["latency"])
	}
	if fields["created_at"] != now {
		t.Fatalf("created_at = %v, want %v", fields["created_at"], now)
	}
}

func TestEventDirectFieldMethodsNilEvent(t *testing.T) {
	var event *Event

	if event.Add("k", "v") {
		t.Fatal("Add returned true for nil event")
	}
	if event.Add2("a", 1, "b", 2) {
		t.Fatal("Add2 returned true for nil event")
	}
	if event.AddField(String("k", "v")) {
		t.Fatal("AddField returned true for nil event")
	}
	if event.AddFields(String("k", "v")) {
		t.Fatal("AddFields returned true for nil event")
	}
	if event.AddString("k", "v") {
		t.Fatal("AddString returned true for nil event")
	}
	if event.Add2Strings("a", "1", "b", "2") {
		t.Fatal("Add2Strings returned true for nil event")
	}
	if event.AddInt("k", 1) {
		t.Fatal("AddInt returned true for nil event")
	}
	if event.AddInt64("k", 1) {
		t.Fatal("AddInt64 returned true for nil event")
	}
	if event.AddFloat64("k", 1.5) {
		t.Fatal("AddFloat64 returned true for nil event")
	}
	if event.AddBool("k", true) {
		t.Fatal("AddBool returned true for nil event")
	}
	if event.AddDuration("k", time.Second) {
		t.Fatal("AddDuration returned true for nil event")
	}
	if event.AddTime("k", time.Now()) {
		t.Fatal("AddTime returned true for nil event")
	}
	if event.SetRoute("/x") {
		t.Fatal("SetRoute returned true for nil event")
	}
	if event.Error(errors.New("boom")) {
		t.Fatal("Error returned true for nil event")
	}
	if event.SetMessage("msg") {
		t.Fatal("SetMessage returned true for nil event")
	}
	if event.SetLevel(LevelInfo) {
		t.Fatal("SetLevel returned true for nil event")
	}
}

func TestEventDirectDuplicateFieldsUseLastValue(t *testing.T) {
	_, event := NewContext(context.Background())

	if !event.Add2Strings("same", "old", "same", "new") {
		t.Fatal("Add2Strings returned false")
	}
	if got := EventFields(event)["same"]; got != "new" {
		t.Fatalf("same = %v, want new", got)
	}

	if !event.Add2("same", "older", "same", "newer") {
		t.Fatal("Add2 returned false")
	}
	if got := EventFields(event)["same"]; got != "newer" {
		t.Fatalf("same = %v, want newer", got)
	}
}

func TestEventSetRouteIgnoresEmptyRoute(t *testing.T) {
	_, event := NewContext(context.Background())

	if event.SetRoute("") {
		t.Fatal("SetRoute returned true for an empty route")
	}
	if fields := EventFields(event); fields != nil {
		t.Fatalf("fields = %#v, want nil", fields)
	}
}

func TestEventDirectLifecycleMetadataMethods(t *testing.T) {
	_, event := NewContext(context.Background())

	if !event.SetMessage("request_complete") {
		t.Fatal("SetMessage returned false")
	}
	if !event.SetLevel(LevelWarn) {
		t.Fatal("SetLevel returned false")
	}
	if event.SetLevel(Level("invalid")) {
		t.Fatal("SetLevel returned true for invalid level")
	}
	if !event.Error(errors.New("boom")) {
		t.Fatal("Error returned false")
	}

	if EventMessage(event) != "request_complete" {
		t.Fatalf("message = %q, want request_complete", EventMessage(event))
	}
	if !EventHasError(event) {
		t.Fatal("expected event to have error")
	}
	level, ok := event.requestedLevelValue()
	if !ok || level != LevelWarn {
		t.Fatalf("level = %s, ok = %t; want WARN true", level, ok)
	}
	if _, ok := EventFields(event)["error"].(map[string]any); !ok {
		t.Fatal("expected structured error field")
	}
}
