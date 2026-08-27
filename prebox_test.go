package hc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestDomainAnyProducesPlainStrings(t *testing.T) {
	cases := map[Domain]string{
		DomainHTTP:         "http",
		DomainJob:          "job",
		DomainMessage:      "msg",
		DomainCLI:          "cli",
		defaultDomainValue: "operation",
		"custom":           "custom",
		"":                 "", // fast path never sees "" (normalized first), but must not panic
	}
	for domain, want := range cases {
		got := domainAny(domain)
		if _, ok := got.(string); !ok {
			t.Fatalf("domainAny(%q) dynamic type = %T, want string", domain, got)
		}
		if got != any(want) {
			t.Fatalf("domainAny(%q) = %#v, want %q", domain, got, want)
		}
	}
}

func TestOutcomeAnyProducesPlainStrings(t *testing.T) {
	cases := map[Outcome]string{
		OutcomeSuccess:  "success",
		OutcomeFailure:  "failure",
		OutcomePanic:    "panic",
		OutcomeCanceled: "canceled",
		OutcomeTimeout:  "timeout",
		OutcomeRetry:    "retry",
		Outcome("weird"): "weird",
	}
	for outcome, want := range cases {
		got := outcomeAny(outcome)
		if _, ok := got.(string); !ok {
			t.Fatalf("outcomeAny(%q) dynamic type = %T, want string", outcome, got)
		}
		if got != any(want) {
			t.Fatalf("outcomeAny(%q) = %#v, want %q", outcome, got, want)
		}
	}
}

func TestOperationFieldsRemainPlainValues(t *testing.T) {
	sink := NewTestSink()
	cfg := Config{Sink: sink, SamplingRate: 1}

	op := StartOperation(context.Background(), OperationStart{
		Domain:      DomainJob,
		Name:        "cleanup",
		ID:          "job_1",
		Source:      "batch",
		Attempt:     1,
		MaxAttempts: 3,
	})
	var err error
	op.End(cfg, &err)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("captured %d events, want 1", len(events))
	}
	fields := events[0].Fields

	assertions := []struct {
		key  string
		want any
	}{
		{"op.domain", "job"},
		{"op.outcome", "success"},
		{"op.name", "cleanup"},
		{"op.id", "job_1"},
		{"op.source", "batch"},
		{"op.attempt", 1},
		{"op.max_attempts", 3},
	}
	for _, a := range assertions {
		got, ok := fields[a.key]
		if !ok {
			t.Fatalf("missing field %q", a.key)
		}
		if got != a.want {
			t.Fatalf("field %q = %#v, want %#v", a.key, got, a.want)
		}
		switch a.want.(type) {
		case string:
			if _, ok := got.(string); !ok {
				t.Fatalf("field %q dynamic type = %T, want string", a.key, got)
			}
		}
	}

	if s := fmt.Sprint(fields["op.outcome"]); s != "success" {
		t.Fatalf("fmt.Sprint(op.outcome) = %q", s)
	}
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["op.domain"] != "job" {
		t.Fatalf("json op.domain = %#v, want \"job\"", decoded["op.domain"])
	}
	if decoded["op.outcome"] != "success" {
		t.Fatalf("json op.outcome = %#v, want \"success\"", decoded["op.outcome"])
	}
}
