package hc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOperationFieldMethodsRecordFields(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	when := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	if !op.AddString("worker", "payments") {
		t.Fatal("AddString returned false")
	}
	if !op.Add2("account_id", "acct_1", "queue", "nightly") {
		t.Fatal("Add2 returned false")
	}
	if !op.AddStrings("a", "1", "b", "2") {
		t.Fatal("AddStrings returned false")
	}
	if !op.Add2Strings("region", "us", "tier", "pro") {
		t.Fatal("Add2Strings returned false")
	}
	if !op.AddInt("attempt", 2) {
		t.Fatal("AddInt returned false")
	}
	if !op.AddBool("cached", true) {
		t.Fatal("AddBool returned false")
	}
	if !op.AddDuration("latency", 3*time.Millisecond) {
		t.Fatal("AddDuration returned false")
	}
	if !op.AddTime("scheduled_at", when) {
		t.Fatal("AddTime returned false")
	}

	fields := EventFields(op.Event())
	assertField(t, fields, "worker", "payments")
	assertField(t, fields, "account_id", "acct_1")
	assertField(t, fields, "queue", "nightly")
	assertField(t, fields, "a", "1")
	assertField(t, fields, "b", "2")
	assertField(t, fields, "region", "us")
	assertField(t, fields, "tier", "pro")
	assertField(t, fields, "attempt", 2)
	assertField(t, fields, "cached", true)
	assertField(t, fields, "latency", 3*time.Millisecond)
	assertField(t, fields, "scheduled_at", when)
}

func TestOperationMethodsRecordErrorMessageAndLevel(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	err := errors.New("boom")

	if !op.Error(err) {
		t.Fatal("Error returned false")
	}
	if !op.SetMessage("cleanup_failed") {
		t.Fatal("SetMessage returned false")
	}
	if !op.SetLevel(LevelWarn) {
		t.Fatal("SetLevel returned false")
	}

	if !EventHasError(op.Event()) {
		t.Fatal("expected event to have error")
	}
	if EventMessage(op.Event()) != "cleanup_failed" {
		t.Fatalf("message = %q, want cleanup_failed", EventMessage(op.Event()))
	}
	if level, ok := op.Event().requestedLevelValue(); !ok || level != LevelWarn {
		t.Fatalf("level = %q (ok=%v), want WARN", level, ok)
	}
}

func TestOperationFieldMethodsNilOperation(t *testing.T) {
	var op *Operation

	if op.Add("k", "v") {
		t.Fatal("Add returned true on nil op")
	}
	if op.Add2("a", "1", "b", "2") {
		t.Fatal("Add2 returned true on nil op")
	}
	if op.AddString("k", "v") {
		t.Fatal("AddString returned true on nil op")
	}
	if op.AddStrings("k", "v") {
		t.Fatal("AddStrings returned true on nil op")
	}
	if op.AddFields(String("k", "v")) {
		t.Fatal("AddFields returned true on nil op")
	}
	if op.Error(errors.New("boom")) {
		t.Fatal("Error returned true on nil op")
	}
	if op.SetMessage("x") {
		t.Fatal("SetMessage returned true on nil op")
	}
	if op.SetLevel(LevelInfo) {
		t.Fatal("SetLevel returned true on nil op")
	}
}

func TestOperationAddStringsRejectsOddPairs(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	if op.AddStrings("a", "1", "b") {
		t.Fatal("AddStrings returned true for odd kv")
	}
}

func assertField(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	if fields[key] != want {
		t.Fatalf("%s = %v, want %v", key, fields[key], want)
	}
}
