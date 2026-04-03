package workerhappycontext

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
)

func TestStartAddsWorkerFields(t *testing.T) {
	scheduledAt := time.Date(2026, 2, 10, 8, 30, 0, 0, time.UTC)
	ctx, event := Start(context.Background(), JobMeta{
		Name:        "cleanup",
		ID:          "job_1",
		Queue:       "nightly",
		Attempt:     2,
		MaxAttempts: 5,
		ScheduledAt: scheduledAt,
	})
	if ctx == nil || event == nil {
		t.Fatal("expected context and event")
	}

	fields := hc.EventFields(event)
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
	ctx, event := Start(context.Background(), JobMeta{Name: "cleanup", ID: "job_1", Queue: "nightly"})
	sink := hc.NewTestSink()

	if !Finish(hc.Config{Sink: sink, SamplingRate: 1}, ctx, event, JobMeta{
		Name:  "cleanup",
		ID:    "job_1",
		Queue: "nightly",
	}, nil, nil) {
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
		ctx, event := Start(context.Background(), JobMeta{Name: "cleanup"})
		sink := hc.NewTestSink()
		if !Finish(hc.Config{Sink: sink, SamplingRate: 0}, ctx, event, JobMeta{Name: "cleanup"}, errors.New("boom"), nil) {
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
		ctx, event := Start(context.Background(), JobMeta{Name: "cleanup"})
		sink := hc.NewTestSink()
		if !Finish(hc.Config{Sink: sink, SamplingRate: 0}, ctx, event, JobMeta{Name: "cleanup"}, nil, "panic-value") {
			t.Fatal("expected panic to bypass sampling")
		}
		ev := sink.Events()[0]
		if ev.Fields["op.outcome"] != string(hc.OutcomePanic) {
			t.Fatalf("outcome = %v", ev.Fields["op.outcome"])
		}
		if _, ok := ev.Fields["panic"].(map[string]any); !ok {
			t.Fatal("expected panic metadata")
		}
	})
}

func TestFinishGuards(t *testing.T) {
	ctx, event := Start(context.Background(), JobMeta{Name: "cleanup"})
	if Finish(hc.Config{}, ctx, event, JobMeta{Name: "cleanup"}, nil, nil) {
		t.Fatal("expected false without sink")
	}
	if Finish(hc.Config{Sink: hc.NewTestSink()}, nil, event, JobMeta{Name: "cleanup"}, nil, nil) {
		t.Fatal("expected false with nil context")
	}
	if Finish(hc.Config{Sink: hc.NewTestSink()}, ctx, nil, JobMeta{Name: "cleanup"}, nil, nil) {
		t.Fatal("expected false with nil event")
	}
}
