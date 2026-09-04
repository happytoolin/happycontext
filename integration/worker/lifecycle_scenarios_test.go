package workerhc

// P8.2 worker lifecycle scenario battery (dst-research §8.2): the
// job-shaped scenarios assembled as end-to-end flows with real
// contexts — retries with attempt metadata, cancellation, deadlines,
// retryable errors, panics, nil runtimes, scheduled_at preservation,
// and consecutive jobs over the same pool. Each scenario drives the
// real worker Start + a deferred End(&err) closure (the documented
// usage shape) and asserts the captured event.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
)

// TestWorkerRetryMetadata: attempt/max_attempts survive to the wire
// through op.*, and a retryable error produces a failure outcome with
// the error level.
func TestWorkerRetryMetadata(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := errors.New("retryable")
	op := Start(ctx, rt, JobMeta{Name: "sync", Attempt: 3, MaxAttempts: 5})
	op.End(&err)

	ev := ts.Events()[0]
	for _, tc := range []struct {
		key  string
		want any
	}{
		{"op.attempt", int64(3)},
		{"op.max_attempts", int64(5)},
	} {
		if v, ok := ev.Lookup(tc.key); !ok || v != tc.want {
			t.Fatalf("%s = %v (%v), want %v", tc.key, v, ok, tc.want)
		}
	}
	if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomeFailure) {
		t.Fatalf("outcome = %v, want failure", v)
	}
	if ev.Level() != hc.LevelError {
		t.Fatalf("level = %v, want error", ev.Level())
	}
	if e, ok := ev.Lookup("error"); !ok {
		t.Fatal("missing error field")
	} else if em := e.(map[string]any); em["message"] != "retryable" {
		t.Fatalf("error.message = %v", em["message"])
	}
}

// TestWorkerCancellation: a canceled context yields OutcomeCanceled
// with the error bypass (never sampled away, even at rate 0).
func TestWorkerCancellation(t *testing.T) {
	for _, rate := range []float64{1, 0} { // error bypass at any rate
		ts := hc.NewTestSink()
		rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: rate})
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // canceled before the job starts
		err := context.Canceled
		op := Start(ctx, rt, JobMeta{Name: "cancel"})
		op.End(&err)
		if len(ts.Events()) != 1 {
			t.Fatalf("rate %v: canceled event dropped", rate)
		}
		ev := ts.Events()[0]
		if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomeCanceled) {
			t.Fatalf("rate %v: outcome = %v, want canceled", rate, v)
		}
		if ev.Level() != hc.LevelError {
			t.Fatalf("rate %v: level = %v", rate, ev.Level())
		}
	}
}

// TestWorkerDeadline: an expired context deadline yields
// OutcomeTimeout with the error bypass.
func TestWorkerDeadline(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // let the deadline expire
	err := context.DeadlineExceeded
	op := Start(ctx, rt, JobMeta{Name: "slow"})
	op.End(&err)
	ev := ts.Events()[0]
	if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomeTimeout) {
		t.Fatalf("outcome = %v, want timeout", v)
	}
}

// TestWorkerJobPanic: a panic in the job function yields
// OutcomePanic + the panic field, re-panics the original value, and
// records error level.
func TestWorkerJobPanic(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})

	repanicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				repanicked = r == "worker-boom"
			}
		}()
		op := Start(context.Background(), rt, JobMeta{Name: "boom-job"})
		defer op.End(nil) // direct defer observes the panic
		panic("worker-boom")
	}()
	if !repanicked {
		t.Fatal("the panic did not propagate")
	}
	ev := ts.Events()[0]
	if v, _ := ev.Lookup("op.outcome"); v != string(hc.OutcomePanic) {
		t.Fatalf("outcome = %v", v)
	}
	if ev.Level() != hc.LevelError {
		t.Fatalf("level = %v", ev.Level())
	}
	if p, ok := ev.Lookup("panic"); !ok {
		t.Fatal("missing panic field")
	} else if pm := p.(map[string]any); pm["value"] != "worker-boom" {
		t.Fatalf("panic.value = %v", pm["value"])
	}
}

// TestWorkerNilRuntime: with a nil runtime the job still runs; nothing
// emits.
func TestWorkerNilRuntime(t *testing.T) {
	ran := false
	op := Start(context.Background(), nil, JobMeta{Name: "noop"})
	var err error
	func() {
		defer op.End(&err)
		ran = true
	}()
	if !ran {
		t.Fatal("job did not run")
	}
	if op.End(&err) {
		t.Fatal("nil runtime emitted")
	}
}

// TestWorkerScheduledAtPreserved: job.scheduled_at survives to the
// wire as the RFC3339 string of the UTC instant.
func TestWorkerScheduledAtPreserved(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	scheduled := time.Date(2026, 2, 10, 8, 30, 0, 0, time.FixedZone("+05:30", 5*3600+1800))
	op := Start(context.Background(), rt, JobMeta{Name: "sched", ScheduledAt: scheduled})
	op.End(nil)
	ev := ts.Events()[0]
	// Lookup returns the typed value (KindTime); the worker stores the
	// UTC instant.
	if v, ok := ev.Lookup("job.scheduled_at"); !ok {
		t.Fatal("missing job.scheduled_at")
	} else if tm, isTime := v.(time.Time); !isTime || !tm.Equal(scheduled.UTC()) {
		t.Fatalf("job.scheduled_at = %v (%T), want the UTC instant %v", v, v, scheduled.UTC())
	}
	// the wire rendering of KindTime fields is RFC3339 (pinned by the
	// core round-trip suites); the typed value is what survives here
	_ = ev
}

// TestWorkerConsecutiveJobs: consecutive jobs on one runtime recycle
// the pooled event between them; each event carries only its own
// fields.
func TestWorkerConsecutiveJobs(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	const jobs = 32
	for i := 0; i < jobs; i++ {
		op := Start(context.Background(), rt, JobMeta{Name: "job", ID: string(rune('a' + i))})
		hc.Add(op.Context(), "index", i)
		var err error
		op.End(&err)
	}
	events := ts.Events()
	if len(events) != jobs {
		t.Fatalf("captured %d events, want %d", len(events), jobs)
	}
	for i, ev := range events {
		if v, _ := ev.Lookup("index"); v != int64(i) {
			t.Fatalf("event %d index = %v — pool cross-contamination", i, v)
		}
		if v, _ := ev.Lookup("op.name"); v != "job" {
			t.Fatalf("event %d name = %v", i, v)
		}
	}
}

// TestWorkerConcurrentJobs: many workers over one runtime, each with
// its own request-confined event (the -race pin for the pooled WAL).
func TestWorkerConcurrentJobs(t *testing.T) {
	ts := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: ts, SamplingRate: 1})
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				op := Start(context.Background(), rt, JobMeta{Name: "worker", ID: "w"})
				hc.Add(op.Context(), "worker", w, "seq", i)
				var err error
				op.End(&err)
			}
		}(w)
	}
	wg.Wait()
	if got := len(ts.Events()); got != 1200 {
		t.Fatalf("captured %d events, want 1200", got)
	}
	for _, ev := range ts.Events() {
		if _, ok := ev.Lookup("worker"); !ok {
			t.Fatal("event lost its worker field")
		}
		if _, ok := ev.Lookup("seq"); !ok {
			t.Fatal("event lost its seq field")
		}
	}
}
