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
	op := Start(context.Background(), nil, JobMeta{Name: "cleanup"}) // nil rt: valid no-op handle
	if op == nil || op.Context() == nil {
		t.Fatal("expected operation handle and context")
	}

	// in-flight fields are observable through the sampler view
	var fields map[string]any
	capture := hc.MustCompile(hc.Config{
		Sink:         hc.NewTestSink(),
		SamplingRate: 1,
		Sampler: func(in hc.SampleInput) bool {
			fields = map[string]any{}
			for _, f := range in.Fields() {
				if v, ok := in.Lookup(f.Key()); ok {
					fields[f.Key()] = v
				}
			}
			return true
		},
	})
	op2 := Start(context.Background(), capture, JobMeta{
		Name:        "cleanup",
		ID:          "job_1",
		Queue:       "nightly",
		Attempt:     2,
		MaxAttempts: 5,
		ScheduledAt: scheduledAt,
	})
	op2.End(nil)
	if fields == nil {
		t.Fatal("sampler never saw the fields")
	}
	if fields["op.domain"] != string(hc.DomainJob) {
		t.Fatalf("op.domain = %v", fields["op.domain"])
	}
	if fields["op.name"] != "cleanup" {
		t.Fatalf("op.name = %v", fields["op.name"])
	}
	// job.* mirrors dropped with the canonical-field pass: op.name and
	// op.source (queue) carry them
	if fields["op.source"] != "nightly" {
		t.Fatalf("op.source = %v", fields["op.source"])
	}
	if _, hasMirror := fields["job.name"]; hasMirror {
		t.Fatal("job.name mirror still emitted")
	}
	if got, ok := fields["job.scheduled_at"].(time.Time); !ok || !got.Equal(scheduledAt) {
		t.Fatalf("job.scheduled_at = %v", fields["job.scheduled_at"])
	}
}

func TestFinishSuccessDefaultMessage(t *testing.T) {
	sink := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	op := Start(context.Background(), rt, JobMeta{Name: "cleanup", ID: "job_1", Queue: "nightly"})
	var err error

	if !op.End(&err) {
		t.Fatal("expected finish to write")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message() != "operation_completed" {
		t.Fatalf("message = %q", events[0].Message())
	}
	if v, _ := events[0].Lookup("op.outcome"); v != string(hc.OutcomeSuccess) {
		t.Fatalf("op.outcome = %v", v)
	}
}

func TestFinishErrorAndPanic(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		sink := hc.NewTestSink()
		rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 0})
		op := Start(context.Background(), rt, JobMeta{Name: "cleanup"})
		err := errors.New("boom")
		if !op.End(&err) {
			t.Fatal("expected error to bypass sampling")
		}
		ev := sink.Events()[0]
		if ev.Level() != hc.LevelError {
			t.Fatalf("level = %v, want ERROR", ev.Level())
		}
		if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomeFailure) {
			t.Fatalf("outcome = %v", v)
		}
	})

	t.Run("panic", func(t *testing.T) {
		sink := hc.NewTestSink()
		rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 0})
		op := Start(context.Background(), rt, JobMeta{Name: "cleanup"})
		func() {
			var err error
			defer func() {
				recovered := recover()
				if recovered != "panic-value" {
					t.Fatalf("recovered = %v, want panic-value", recovered)
				}
			}()
			defer op.End(&err) // direct defer: observes the panic
			panic("panic-value")
		}()
		ev := sink.Events()[0]
		if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomePanic) {
			t.Fatalf("outcome = %v", v)
		}
		if p, ok := ev.Lookup("panic"); !ok {
			t.Fatal("expected panic metadata")
		} else if _, isMap := p.(map[string]any); !isMap {
			t.Fatalf("panic metadata = %T", p)
		}
	})
}

func TestEndGuards(t *testing.T) {
	rt := hc.MustCompile(hc.Config{})
	op := Start(context.Background(), rt, JobMeta{Name: "cleanup"})
	var err error
	if op.End(&err) {
		t.Fatal("expected false without sink")
	}
	var nilOp *hc.Operation
	if nilOp.End(&err) {
		t.Fatal("expected false with nil operation")
	}
}
