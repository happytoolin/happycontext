package hc

// Agent B — concurrency and straggler hammering. The existing suites
// cover post-seal stragglers on single events; these charter load:
// many requests recycling a small pool while stragglers fire, retained
// records surviving recycling, one Runtime shared across domains, and
// concurrent End against a slow sink.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// High-volume straggler storm over a recycled pool: every request
// spawns stragglers that write after End returns, while the pool keeps
// recycling the same few events. No request may ever observe another
// request's fields (generation guard), and counts must be exact.
func TestCrashStragglerStormOverRecycledPool(t *testing.T) {
	rt, ts := testRT(t, nil)
	const workers = 16
	const perWorker = 60

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWorker {
				op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
				mine := fmt.Sprintf("w%d-i%d", w, i)
				Add(op.Context(), "mine", mine)
				_ = op.End(nil)
				// Stragglers begin after End returned: sealed no-ops.
				var sw sync.WaitGroup
				for s := range 4 {
					sw.Add(1)
					go func(s int) {
						defer sw.Done()
						Add(op.Context(), "straggler", fmt.Sprintf("w%d-i%d-s%d", w, i, s))
					}(s)
				}
				sw.Wait()
			}
		}(w)
	}
	wg.Wait()

	evs := ts.Events()
	if len(evs) != workers*perWorker {
		t.Fatalf("events = %d, want %d", len(evs), workers*perWorker)
	}
	for _, ev := range evs {
		if _, ok := ev.Lookup("straggler"); ok {
			t.Fatal("post-seal straggler field reached the wire")
		}
		mine, _ := ev.Lookup("mine")
		if s, ok := mine.(string); !ok || !strings.HasPrefix(s, "w") {
			t.Fatalf("foreign or corrupt field: %#v", mine)
		}
	}
}

// Armed-mode variant: the watchdog protocol path must give the same
// guarantees under the same storm.
func TestCrashStragglerStormArmed(t *testing.T) {
	rt, ts := testRT(t, nil)
	const workers = 8
	const perWorker = 40
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWorker {
				op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
				op.ev.arm()
				Add(op.Context(), "mine", fmt.Sprintf("a%d-%d", w, i))
				_ = op.End(nil)
				Add(op.Context(), "straggler", 1)
			}
		}(w)
	}
	wg.Wait()
	evs := ts.Events()
	if len(evs) != workers*perWorker {
		t.Fatalf("events = %d, want %d", len(evs), workers*perWorker)
	}
	for _, ev := range evs {
		if _, ok := ev.Lookup("straggler"); ok {
			t.Fatal("armed straggler field reached the wire")
		}
	}
}

// A realistic pipeline retains the ENCODED bytes (computed during
// Write, cached on the record). Recycling must never change what a
// retained record yields from later Encoded() calls.
func TestCrashRetainedEncodedBytesSurviveRecycling(t *testing.T) {
	rt, _ := testRT(t, nil)
	retain := &retainingSink{}
	rt2 := MustCompile(Config{Sink: retain, SamplingRate: 1})

	for i := range 300 {
		op := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "j"})
		Add(op.Context(), "seq", i)
		_ = op.End(nil)
	}
	if len(retain.records) != 300 {
		t.Fatalf("records = %d, want 300", len(retain.records))
	}
	for i, rec := range retain.records {
		line := string(rec.Encoded())
		want := fmt.Sprintf(`"seq":%d`, i)
		if !strings.Contains(line, want) {
			t.Fatalf("record %d mutated after recycling: %s", i, line)
		}
	}
	_ = rt
}

type retainingSink struct {
	records []*Record
}

func (r *retainingSink) Write(_ context.Context, rec *Record) {
	rec.Encoded() // cache the bytes while the record is valid
	r.records = append(r.records, rec)
}

// One immutable Runtime shared by concurrent operations across every
// domain, with policies and level rates compiled in — no shared-state
// races (pinned by -race) and exact counts.
func TestCrashSharedRuntimeAllDomains(t *testing.T) {
	rt, ts := testRT(t, func(c *Config) {
		c.LevelSamplingRates = map[Level]float64{LevelInfo: 1, LevelError: 1}
		c.OperationPolicies = map[Domain]OperationPolicy{
			DomainJob: {SamplingRate: ptrRate(1.0)},
		}
	})
	domains := []Domain{DomainHTTP, DomainJob, DomainMessage, DomainCLI, "custom"}
	var wg sync.WaitGroup
	for d := range domains {
		wg.Add(1)
		go func(d int) {
			defer wg.Done()
			for i := range 50 {
				op := Start(context.Background(), rt, OperationStart{Domain: domains[d], Name: "x"})
				Add(op.Context(), "i", i)
				if i%7 == 0 {
					Error(op.Context(), fmt.Errorf("boom %d", i))
				}
				_ = op.End(nil)
			}
		}(d)
	}
	wg.Wait()
	evs := ts.Events()
	if len(evs) != len(domains)*50 {
		t.Fatalf("events = %d, want %d (errors bypass sampling)", len(evs), len(domains)*50)
	}
}

func ptrRate(r float64) *float64 { return &r }

// Concurrent first-End callers against a slow sink: exactly one
// commit, every caller observes the same result.
func TestCrashConcurrentEndSlowSink(t *testing.T) {
	slow := &slowSink{d: 3 * time.Millisecond}
	rt := MustCompile(Config{Sink: slow, SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	const racers = 16
	res := make([]bool, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i] = op.End(nil)
		}(i)
	}
	wg.Wait()
	for i, r := range res {
		if i > 0 && r != res[0] {
			t.Fatalf("racer %d observed %v, racer 0 observed %v", i, r, res[0])
		}
	}
	if slow.writes != 1 {
		t.Fatalf("sink writes = %d, want 1", slow.writes)
	}
}

type slowSink struct {
	d      time.Duration
	writes int
}

func (s *slowSink) Write(_ context.Context, rec *Record) {
	time.Sleep(s.d)
	s.writes++
}

// A context handed to a spawned goroutine keeps writing to ITS OWN
// operation (innermost attach wins) — the sibling operation must stay
// untouched.
func TestCrashForeignCtxIsolation(t *testing.T) {
	rt, ts := testRT(t, nil)
	opA := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	opB := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		Add(opA.Context(), "fromGoroutine", "A")
	}()
	<-done
	Add(opB.Context(), "belongs", "B")

	_ = opA.End(nil)
	_ = opB.End(nil)
	for _, ev := range ts.Events() {
		if v, ok := ev.Lookup("fromGoroutine"); ok {
			if _, also := ev.Lookup("belongs"); also {
				t.Fatal("field bled across operations")
			}
			if v != "A" {
				t.Fatalf("fromGoroutine = %v", v)
			}
		}
	}
}
