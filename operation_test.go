package hc

import (
	"context"
	"errors"
	"testing"
)

func TestBeginOperationAddsEnvelopeFields(t *testing.T) {
	var parent context.Context
	ctx, event := BeginOperation(parent, OperationStart{
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

func TestStartOperationProvidesHandle(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	if op == nil {
		t.Fatal("expected operation handle")
	}
	if op.Context() == nil {
		t.Fatal("expected operation context")
	}
	if op.Event() == nil {
		t.Fatal("expected operation event")
	}
}

func TestStartOperationLocalDoesNotStoreEventInContext(t *testing.T) {
	op := StartOperationLocal(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	if op.Context() == nil {
		t.Fatal("expected operation context")
	}
	if op.Event() == nil {
		t.Fatal("expected operation event")
	}
	if FromContext(op.Context()) != nil {
		t.Fatal("local operation should not store event in context")
	}

	fields := EventFields(op.Event())
	if _, ok := fields["op.domain"]; ok {
		t.Fatal("local operation should defer op.domain until write")
	}

	sink := NewTestSink()
	var err error
	if !op.End(Config{Sink: sink, SamplingRate: 1}, &err) {
		t.Fatal("expected local operation to write")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Fields["op.domain"] != string(DomainJob) {
		t.Fatalf("op.domain = %v, want %s", events[0].Fields["op.domain"], DomainJob)
	}
	if events[0].Fields["op.name"] != "cleanup" {
		t.Fatalf("op.name = %v, want cleanup", events[0].Fields["op.name"])
	}
}

func TestStartOperationInPlaceUsesCallerEventStorage(t *testing.T) {
	var event Event
	op := StartOperationInPlace(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"}, &event)
	if op.Event() != &event {
		t.Fatal("expected operation to use caller event")
	}
	if FromContext(op.Context()) != nil {
		t.Fatal("in-place operation should not store event in context")
	}

	op.AddString("worker", "payments")
	fields := EventFields(&event)
	if fields["worker"] != "payments" {
		t.Fatalf("worker = %v, want payments", fields["worker"])
	}

	op = StartOperationInPlace(context.Background(), OperationStart{Domain: DomainJob, Name: "again"}, &event)
	fields = EventFields(&event)
	if _, ok := fields["worker"]; ok {
		t.Fatal("expected reused event fields to be cleared")
	}
	if op.Event() != &event {
		t.Fatal("expected reused operation to use caller event")
	}
}

func TestLocalOperationsUseMonotonicOnlyStartTime(t *testing.T) {
	local := StartOperationLocal(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	if !EventStartTime(local.Event()).IsZero() {
		t.Fatal("expected local operation wall-clock start time to be zero")
	}

	var event Event
	inPlace := StartOperationInPlace(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"}, &event)
	if !EventStartTime(inPlace.Event()).IsZero() {
		t.Fatal("expected in-place operation wall-clock start time to be zero")
	}
}

func TestOperationFinishFinalizesWithoutDeferredEnd(t *testing.T) {
	op := StartOperationLocal(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	op.AddString("worker", "payments")

	sink := NewTestSink()
	if !op.Finish(Config{Sink: sink, SamplingRate: 1}, nil) {
		t.Fatal("expected Finish to write")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["worker"] != "payments" {
		t.Fatalf("worker = %v, want payments", events[0].Fields["worker"])
	}
	if events[0].Fields["op.outcome"] != string(OutcomeSuccess) {
		t.Fatalf("op.outcome = %v, want success", events[0].Fields["op.outcome"])
	}
}

func TestOperationFinishPreparedUsesNormalizedConfig(t *testing.T) {
	op := StartOperationLocal(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	cfg := PrepareConfig(Config{Sink: NewTestSink(), SamplingRate: 2})

	if !op.FinishPrepared(cfg, nil) {
		t.Fatal("expected FinishPrepared to use normalized sampling rate")
	}
}

func TestFinishPreparedOperationUsesPreparedConfigAndCompleteStart(t *testing.T) {
	ctx, event := NewContext(context.Background())
	AddString(ctx, "worker", "payments")
	sink := NewTestSink()
	prepared := PrepareConfig(Config{Sink: sink, SamplingRate: 2})

	ok := FinishPreparedOperation(prepared, OperationFinish{
		Ctx:           ctx,
		Event:         event,
		Start:         OperationStart{Domain: DomainJob, Name: "cleanup"},
		StartComplete: true,
	})
	if !ok {
		t.Fatal("expected prepared operation finish to write")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["worker"] != "payments" {
		t.Fatalf("worker = %v, want payments", events[0].Fields["worker"])
	}
	if events[0].Fields["op.domain"] != string(DomainJob) {
		t.Fatalf("op.domain = %v, want %s", events[0].Fields["op.domain"], DomainJob)
	}
	if events[0].Fields["op.name"] != "cleanup" {
		t.Fatalf("op.name = %v, want cleanup", events[0].Fields["op.name"])
	}
}

func TestFinishPreparedOperationCompleteStartDefaultSampling(t *testing.T) {
	t.Run("drops healthy sampled out event", func(t *testing.T) {
		ctx, event := NewContext(context.Background())
		sink := NewTestSink()
		prepared := PrepareConfig(Config{Sink: sink, SamplingRate: 0})

		ok := FinishPreparedOperation(prepared, OperationFinish{
			Ctx:           ctx,
			Event:         event,
			Start:         OperationStart{Domain: DomainHTTP, Name: "request"},
			StartComplete: true,
			Code:          200,
		})
		if ok {
			t.Fatal("expected healthy sampled-out event to drop")
		}
		if len(sink.Events()) != 0 {
			t.Fatalf("expected no events, got %d", len(sink.Events()))
		}
	})

	t.Run("keeps failed sampled out event", func(t *testing.T) {
		ctx, event := NewContext(context.Background())
		sink := NewTestSink()
		prepared := PrepareConfig(Config{Sink: sink, SamplingRate: 0})

		ok := FinishPreparedOperation(prepared, OperationFinish{
			Ctx:           ctx,
			Event:         event,
			Start:         OperationStart{Domain: DomainHTTP, Name: "request"},
			StartComplete: true,
			Code:          500,
		})
		if !ok {
			t.Fatal("expected failed event to write")
		}
		if len(sink.Events()) != 1 {
			t.Fatalf("expected 1 event, got %d", len(sink.Events()))
		}
	})
}

func TestPreparedOperationStartStartsOperations(t *testing.T) {
	prepared := PrepareOperationStart(OperationStart{Domain: DomainJob, Name: "cleanup"})

	local := prepared.StartLocalContext(context.Background())
	if local.Context() == nil {
		t.Fatal("expected local prepared operation context")
	}
	if local.Event() == nil {
		t.Fatal("expected local prepared operation event")
	}
	if local.start.Name != "cleanup" {
		t.Fatalf("start name = %q, want cleanup", local.start.Name)
	}

	var event Event
	inPlace := prepared.StartInPlaceContext(context.Background(), &event)
	if inPlace.Event() != &event {
		t.Fatal("expected prepared in-place operation to use caller event")
	}
	if inPlace.start.Domain != DomainJob {
		t.Fatalf("start domain = %q, want %q", inPlace.start.Domain, DomainJob)
	}
}

func TestPreparedOperationStartNoTimingEmitsZeroDuration(t *testing.T) {
	prepared := PrepareOperationStart(OperationStart{Domain: DomainJob, Name: "cleanup"})
	cfg := PrepareConfig(Config{Sink: NewTestSink(), SamplingRate: 1})

	var event Event
	op := prepared.StartInPlaceNoTimingContext(context.Background(), &event)
	op.AddString("worker", "payments")

	sink := cfg.Config().Sink.(*TestSink)
	if !op.FinishPrepared(cfg, nil) {
		t.Fatal("expected no-timing operation to write")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["duration_ms"] != int64(0) {
		t.Fatalf("duration_ms = %v, want 0", events[0].Fields["duration_ms"])
	}
}

func TestPreparedOperationStartNoTimingPreservesMessageOverride(t *testing.T) {
	prepared := PrepareOperationStart(OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()
	cfg := PrepareConfig(Config{Sink: sink, SamplingRate: 1})

	var event Event
	op := prepared.StartInPlaceNoTimingContext(context.Background(), &event)
	op.SetMessage("custom_done")

	if !op.FinishPrepared(cfg, nil) {
		t.Fatal("expected no-timing operation to write")
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "custom_done" {
		t.Fatalf("message = %q, want custom_done", events[0].Message)
	}
}

func TestOperationNilAccessors(t *testing.T) {
	var op *Operation

	if op.Context() != nil {
		t.Fatalf("nil operation context = %v, want nil", op.Context())
	}
	if op.Event() != nil {
		t.Fatalf("nil operation event = %v, want nil", op.Event())
	}
}

func TestOperationEndSuccessWritesDefaultOperationMessage(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()
	var err error

	ok := op.End(Config{Sink: sink, SamplingRate: 1}, &err)
	if !ok {
		t.Fatal("expected end to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != DefaultOperationMessage {
		t.Fatalf("message = %q, want %q", events[0].Message, DefaultOperationMessage)
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

func TestOperationEndErrorAndPanic(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "sync"})
		sink := NewTestSink()
		err := errors.New("boom")

		ok := op.End(Config{Sink: sink, SamplingRate: 0}, &err)
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
		op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "sync"})
		sink := NewTestSink()
		func() {
			var err error
			defer func() {
				recovered := recover()
				if recovered != "panic-value" {
					t.Fatalf("recovered = %v, want panic-value", recovered)
				}
			}()
			defer op.End(Config{Sink: sink, SamplingRate: 0}, &err)
			panic("panic-value")
		}()

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

func TestOperationEndAppliesPolicyAndRequestedFloor(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	SetLevel(op.Context(), LevelWarn)
	sink := NewTestSink()
	rate := 2.0
	var err error

	ok := op.End(Config{
		Sink:         sink,
		SamplingRate: 1,
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {
				SuccessLevel: LevelDebug,
				SamplingRate: &rate,
			},
		},
	}, &err)
	if !ok {
		t.Fatal("expected end to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != LevelWarn {
		t.Fatalf("level = %s, want WARN floor", events[0].Level)
	}
}

func TestOperationEndNilGuard(t *testing.T) {
	var op *Operation
	var err error
	if op.End(Config{Sink: NewTestSink()}, &err) {
		t.Fatal("expected false for nil operation")
	}
}

func TestOperationEndUsesErrorPointer(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()
	err := errors.New("boom")

	if !op.End(Config{Sink: sink, SamplingRate: 1}, &err) {
		t.Fatal("expected end to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["op.outcome"] != string(OutcomeFailure) {
		t.Fatalf("outcome = %v", events[0].Fields["op.outcome"])
	}
}

func TestOperationEndCapturesAndRepanics(t *testing.T) {
	op := StartOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()

	func() {
		var err error
		defer func() {
			recovered := recover()
			if recovered != "panic-value" {
				t.Fatalf("recovered = %v, want panic-value", recovered)
			}
		}()
		defer op.End(Config{Sink: sink, SamplingRate: 1}, &err)
		panic("panic-value")
	}()

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["op.outcome"] != string(OutcomePanic) {
		t.Fatalf("outcome = %v", events[0].Fields["op.outcome"])
	}
}

func TestFinishOperationCompatibilityAppliesPolicyAndRequestedFloor(t *testing.T) {
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

func TestFinishOperationDomainSamplingOverridesLevelSampling(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()
	rate := 1.0

	ok := FinishOperation(Config{
		Sink:         sink,
		SamplingRate: 0,
		LevelSamplingRates: map[Level]float64{
			LevelInfo: 0,
		},
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
	if !ok {
		t.Fatal("expected domain sampling override to beat level sampling")
	}
	if len(sink.Events()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.Events()))
	}
}

func TestFinishOperationLevelSamplingAppliesWithoutDomainOverride(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "cleanup"})
	sink := NewTestSink()

	ok := FinishOperation(Config{
		Sink:         sink,
		SamplingRate: 1,
		LevelSamplingRates: map[Level]float64{
			LevelInfo: 0,
		},
	}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainJob, Name: "cleanup"},
	})
	if ok {
		t.Fatal("expected level sampling override to apply when domain has no explicit sampling policy")
	}
	if len(sink.Events()) != 0 {
		t.Fatalf("expected no events, got %d", len(sink.Events()))
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
	if events[0].Message != DefaultMessage {
		t.Fatalf("message = %q, want %q", events[0].Message, DefaultMessage)
	}
}

func TestFinishOperationHTTPStatusFieldAffectsBuiltInSampling(t *testing.T) {
	ctx, event := BeginOperation(context.Background(), OperationStart{Domain: DomainHTTP, Name: "GET /x"})
	Add(ctx, "http.status", 500)

	sink := NewTestSink()
	ok := FinishOperation(Config{Sink: sink, SamplingRate: 0}, OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: OperationStart{Domain: DomainHTTP, Name: "GET /x"},
		Code:  200,
	})
	if !ok {
		t.Fatal("expected http.status field to keep failed HTTP event")
	}
	if len(sink.Events()) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.Events()))
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

func TestFinishOperationAppliesStartFieldsToProvidedEvent(t *testing.T) {
	mismatchedCtx, _ := BeginOperation(context.Background(), OperationStart{Domain: DomainJob, Name: "wrong"})
	_, event := NewContext(context.Background())
	sink := NewTestSink()

	ok := FinishOperation(Config{Sink: sink, SamplingRate: 1}, OperationFinish{
		Ctx:   mismatchedCtx,
		Event: event,
		Start: OperationStart{
			Domain: DomainCLI,
			Name:   "right",
			ID:     "op_1",
		},
	})
	if !ok {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["op.domain"] != string(DomainCLI) {
		t.Fatalf("op.domain = %v, want %s", events[0].Fields["op.domain"], DomainCLI)
	}
	if events[0].Fields["op.name"] != "right" {
		t.Fatalf("op.name = %v, want right", events[0].Fields["op.name"])
	}
	if events[0].Fields["op.id"] != "op_1" {
		t.Fatalf("op.id = %v, want op_1", events[0].Fields["op.id"])
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
