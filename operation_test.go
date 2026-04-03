package hc

import (
	"context"
	"errors"
	"testing"
)

func TestBeginOperationAddsEnvelopeFields(t *testing.T) {
	ctx, event := BeginOperation(nil, OperationStart{
		Domain:      DomainJob,
		Name:        "cleanup",
		ID:          "job_1",
		Source:      "nightly",
		Attempt:     2,
		MaxAttempts: 5,
	})
	if ctx == nil || event == nil {
		t.Fatal("expected context and event")
	}

	fields := EventFields(event)
	if fields["op.domain"] != string(DomainJob) {
		t.Fatalf("op.domain = %v", fields["op.domain"])
	}
	if fields["op.name"] != "cleanup" {
		t.Fatalf("op.name = %v", fields["op.name"])
	}
	if fields["op.id"] != "job_1" {
		t.Fatalf("op.id = %v", fields["op.id"])
	}
	if fields["op.source"] != "nightly" {
		t.Fatalf("op.source = %v", fields["op.source"])
	}
	if fields["op.attempt"] != 2 {
		t.Fatalf("op.attempt = %v", fields["op.attempt"])
	}
	if fields["op.max_attempts"] != 5 {
		t.Fatalf("op.max_attempts = %v", fields["op.max_attempts"])
	}
}

func TestFinishOperationSuccessWritesDefaultOperationMessage(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()

	ok := FinishOperation(Config{Sink: sink, SamplingRate: 1}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainJob, Name: "cleanup"},
		Code:  0,
	})
	if !ok {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != defaultOperationMessage {
		t.Fatalf("message = %q, want %q", events[0].Message, defaultOperationMessage)
	}
	if events[0].Level != LevelInfo {
		t.Fatalf("level = %s, want INFO", events[0].Level)
	}
	if events[0].Fields["op.outcome"] != string(OutcomeSuccess) {
		t.Fatalf("op.outcome = %v", events[0].Fields["op.outcome"])
	}
	if events[0].Fields["op.code"] != 0 {
		t.Fatalf("op.code = %v, want 0", events[0].Fields["op.code"])
	}
}

func TestFinishOperationErrorAndPanic(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "sync"})
		sink := NewTestSink()

		ok := FinishOperation(Config{Sink: sink, SamplingRate: 0}, OperationFinish{
			Ctx:   ctx,
			Event: event,
			Start: OperationStart{Domain: DomainJob, Name: "sync"},
			Err:   errors.New("boom"),
		})
		if !ok {
			t.Fatal("expected errored operation to bypass sampling")
		}

		events := sink.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Level != LevelError {
			t.Fatalf("level = %s, want ERROR", events[0].Level)
		}
		if events[0].Fields["op.outcome"] != string(OutcomeFailure) {
			t.Fatalf("outcome = %v", events[0].Fields["op.outcome"])
		}
		if _, ok := events[0].Fields["error"].(map[string]any); !ok {
			t.Fatal("expected structured error field")
		}
	})

	t.Run("panic", func(t *testing.T) {
		ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "sync"})
		sink := NewTestSink()

		ok := FinishOperation(Config{Sink: sink, SamplingRate: 0}, OperationFinish{
			Ctx:       ctx,
			Event:     event,
			Start:     OperationStart{Domain: DomainJob, Name: "sync"},
			Recovered: "panic-value",
		})
		if !ok {
			t.Fatal("expected panic operation to bypass sampling")
		}

		events := sink.Events()
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Fields["op.outcome"] != string(OutcomePanic) {
			t.Fatalf("outcome = %v", events[0].Fields["op.outcome"])
		}
		if _, ok := events[0].Fields["panic"].(map[string]any); !ok {
			t.Fatal("expected panic metadata")
		}
	})
}

func TestFinishOperationAppliesPolicyAndRequestedFloor(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	SetLevel(ctx, LevelWarn)
	sink := NewTestSink()
	rate := 2.0

	ok := FinishOperation(Config{
		Sink:         sink,
		SamplingRate: 1,
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {
				SuccessLevel: LevelDebug,
				SamplingRate: &rate,
			},
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainJob, Name: "cleanup"},
	})
	if !ok {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != LevelWarn {
		t.Fatalf("level = %s, want WARN floor", events[0].Level)
	}
}

func TestFinishOperationPolicySamplingOverride(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()
	rate := 0.0

	ok := FinishOperation(Config{
		Sink:         sink,
		SamplingRate: 1,
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {
				SamplingRate: &rate,
			},
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainJob, Name: "cleanup"},
	})
	if ok {
		t.Fatal("expected operation policy sampling override to drop healthy event")
	}
	if len(sink.Events()) != 0 {
		t.Fatal("expected no events")
	}
}

func TestFinishOperationHTTPDefaultsAndSamplerCompatibility(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainHTTP, Name: "GET /x"})
	Add(ctx, "http.method", "GET", "http.path", "/x", "http.status", 200)

	var got SampleInput
	sink := NewTestSink()
	ok := FinishOperation(Config{
		Sink: sink,
		Sampler: func(in SampleInput) bool {
			got = in
			return true
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainHTTP, Name: "GET /x"},
		Code:  200,
	})
	if !ok {
		t.Fatal("expected finish to write")
	}
	if got.Domain != DomainHTTP {
		t.Fatalf("domain = %q, want %q", got.Domain, DomainHTTP)
	}
	if got.Method != "GET" || got.Path != "/x" || got.StatusCode != 200 {
		t.Fatalf("legacy fields = %+v", got)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != defaultMessage {
		t.Fatalf("message = %q, want %q", events[0].Message, defaultMessage)
	}
}

func TestFinishOperationAppliesEventMessage(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	SetMessage(ctx, "hello world")
	sink := NewTestSink()

	ok := FinishOperation(Config{Sink: sink, SamplingRate: 1, Message: "default message"}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainJob, Name: "cleanup"},
	})
	if !ok {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "hello world" {
		t.Fatalf("message = %q, want %q", events[0].Message, "hello world")
	}
}

func TestFinishOperationGuardPaths(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	if FinishOperation(Config{}, OperationFinish{Ctx: ctx, Event: event}) {
		t.Fatal("expected false when sink is nil")
	}
	if FinishOperation(Config{Sink: NewTestSink()}, OperationFinish{Ctx: nil, Event: event}) {
		t.Fatal("expected false when ctx is nil")
	}
	if FinishOperation(Config{Sink: NewTestSink()}, OperationFinish{Ctx: ctx, Event: nil}) {
		t.Fatal("expected false when event is nil")
	}
}

func TestFinishOperationUsesExistingStartFieldsWhenMissing(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{
		Domain: DomainJob,
		Name:   "reconcile",
		ID:     "job_2",
	})
	sink := NewTestSink()

	ok := FinishOperation(Config{Sink: sink, SamplingRate: 1}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{},
	})
	if !ok {
		t.Fatal("expected finish to write")
	}
	ev := sink.Events()[0]
	if ev.Fields["op.domain"] != string(DomainJob) {
		t.Fatalf("op.domain = %v", ev.Fields["op.domain"])
	}
	if ev.Fields["op.name"] != "reconcile" {
		t.Fatalf("op.name = %v", ev.Fields["op.name"])
	}
	if ev.Fields["op.id"] != "job_2" {
		t.Fatalf("op.id = %v", ev.Fields["op.id"])
	}
}
