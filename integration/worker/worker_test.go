package workerhc

import (
	"context"
	"errors"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
)

func TestStartAddsWorkerFields(t *testing.T) {
	scheduledAt := time.Date(2026, 2, 10, 8, 30, 0, 0, time.UTC)
	op := Start(context.Background(), JobMeta{
		Name:        "cleanup",
		ID:          "job_1",
		Queue:       "nightly",
		Attempt:     2,
		MaxAttempts: 5,
		ScheduledAt: scheduledAt,
	})
	if op == nil || op.Context() == nil || op.Event() == nil {
		t.Fatal("expected operation handle, context, and event")
	}

	fields := hc.EventFields(op.Event())
	if fields["op.domain"] != string(hc.DomainJob) {
		t.Fatalf("op.domain = %v", fields["op.domain"])
	}
	if fields["op.name"] != "cleanup" {
		t.Fatalf("op.name = %v", fields["op.name"])
	}
	if fields["job.name"] != "cleanup" {
		t.Fatalf("job.name = %v", fields["job.name"])
	}
	if fields["job.queue"] != "nightly" {
		t.Fatalf("job.queue = %v", fields["job.queue"])
	}
	if got, ok := fields["job.scheduled_at"].(time.Time); !ok || !got.Equal(scheduledAt) {
		t.Fatalf("job.scheduled_at = %v", fields["job.scheduled_at"])
	}
}

func TestFinishSuccessDefaultMessage(t *testing.T) {
	op := Start(context.Background(), JobMeta{Name: "cleanup", ID: "job_1", Queue: "nightly"})
	sink := hc.NewTestSink()
	var err error

	if !op.End(hc.Config{Sink: sink, SamplingRate: 1}, &err) {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "operation_completed" {
		t.Fatalf("message = %q", events[0].Message)
	}
	if events[0].Fields["op.outcome"] != string(hc.OutcomeSuccess) {
		t.Fatalf("op.outcome = %v", events[0].Fields["op.outcome"])
	}
}

func TestFinishErrorAndPanic(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		op := Start(context.Background(), JobMeta{Name: "cleanup"})
		sink := hc.NewTestSink()
		err := errors.New("boom")
		if !op.End(hc.Config{Sink: sink, SamplingRate: 0}, &err) {
			t.Fatal("expected error to bypass sampling")
		}
		ev := sink.Events()[0]
		if ev.Level != hc.LevelError {
			t.Fatalf("level = %s, want ERROR", ev.Level)
		}
		if ev.Fields["op.outcome"] != string(hc.OutcomeFailure) {
			t.Fatalf("outcome = %v", ev.Fields["op.outcome"])
		}
	})

	t.Run("panic", func(t *testing.T) {
		op := Start(context.Background(), JobMeta{Name: "cleanup"})
		sink := hc.NewTestSink()
		func() {
			var err error
			defer func() {
				recovered := recover()
				if recovered != "panic-value" {
					t.Fatalf("recovered = %v, want panic-value", recovered)
				}
			}()
			defer op.End(hc.Config{Sink: sink, SamplingRate: 0}, &err)
			panic("panic-value")
		}()
		ev := sink.Events()[0]
		if ev.Fields["op.outcome"] != string(hc.OutcomePanic) {
			t.Fatalf("outcome = %v", ev.Fields["op.outcome"])
		}
		if _, ok := ev.Fields["panic"].(map[string]any); !ok {
			t.Fatal("expected panic metadata")
		}
	})
}

func TestEndGuards(t *testing.T) {
	op := Start(context.Background(), JobMeta{Name: "cleanup"})
	var err error
	if op.End(hc.Config{}, &err) {
		t.Fatal("expected false without sink")
	}
	var nilOp *hc.Operation
	if nilOp.End(hc.Config{Sink: hc.NewTestSink()}, &err) {
		t.Fatal("expected false with nil operation")
	}
}
