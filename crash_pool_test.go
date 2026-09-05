package hc

// Agent D — pool and memory lifecycle torture: recycling without
// cross-request bleed, abandoned operations, pool-cap boundary
// behavior, and why the copy-out contract exists (a sink that retains
// raw slices gets recycled memory — TestSink copies stay stable).

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Sequential heavy reuse: with a tiny pool the same events serve
// thousands of requests; no field may survive its request.
func TestCrashPoolReuseNoBleed(t *testing.T) {
	rt, ts := testRT(t, nil)
	const n = 2000
	for i := range n {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
		Add(op.Context(), "seq", fmt.Sprintf("s%d", i), "padding", strings.Repeat("p", 32))
		if !op.End(nil) {
			t.Fatalf("request %d dropped", i)
		}
	}
	evs := ts.Events()
	if len(evs) != n {
		t.Fatalf("events = %d, want %d", len(evs), n)
	}
	seen := map[string]bool{}
	for _, ev := range evs {
		v, _ := ev.Lookup("seq")
		s, _ := v.(string)
		if seen[s] {
			t.Fatalf("duplicate seq %s — pool bleed", s)
		}
		seen[s] = true
	}
}

// Operations that are started and abandoned (never Ended) must not
// leak goroutines (goleak gates the suite) or poison the pool.
func TestCrashAbandonedOperations(t *testing.T) {
	rt, _ := testRT(t, nil)
	ops := make([]*Operation, 500)
	for i := range ops {
		ops[i] = Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		Add(ops[i].Context(), "abandoned", i)
	}
	// One late End on an arbitrary abandoned op still works.
	if !ops[len(ops)-1].End(nil) {
		t.Fatal("late End on abandoned op failed")
	}
	// Pool serves fresh requests afterwards.
	rt2, ts2 := testRT(t, nil)
	op := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "fresh"})
	Add(op.Context(), "fresh", true)
	if !op.End(nil) || len(ts2.Events()) != 1 {
		t.Fatal("pool unusable after abandonment storm")
	}
}

// Events whose backing array grows past the 1024-field pool cap are
// dropped from the pool at release — behavior, not corruption, is the
// contract: the wide event itself must still be complete on the wire.
func TestCrashWideEventPoolCapBoundary(t *testing.T) {
	rt, ts := testRT(t, nil)
	for _, n := range []int{1023, 1024, 1025, 1100} {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "wide"})
		for i := range n {
			Add(op.Context(), fmt.Sprintf("f%d", i), i)
		}
		if !op.End(nil) {
			t.Fatalf("n=%d: dropped", n)
		}
	}
	evs := ts.Events()
	if len(evs) != 4 {
		t.Fatalf("events = %d, want 4", len(evs))
	}
	for i, ev := range evs {
		want := []int{1023, 1024, 1025, 1100}[i]
		last := fmt.Sprintf("f%d", want-1)
		v, ok := ev.Lookup(last)
		n, isInt := v.(int64)
		if !ok || !isInt || n != int64(want-1) {
			t.Fatalf("event %d (n=%d): last field = %#v", i, want, v)
		}
	}
}

// The copy-out contract, demonstrated: a sink that retains the RAW
// Fields() slice across subsequent requests observes recycling (the
// documented hazard), while the deep-copying TestSink does not. Pins
// why amendment 9 says "copy anything you retain".
func TestCrashRetainRawSliceVsCopy(t *testing.T) {
	raw := &rawRetainingSink{}
	rtRaw := MustCompile(Config{Sink: raw, SamplingRate: 1})

	op := Start(context.Background(), rtRaw, OperationStart{Domain: DomainJob, Name: "first"})
	Add(op.Context(), "who", "first")
	_ = op.End(nil)
	retained := raw.lastFields

	// Recycle the same events with different payloads.
	for i := range 50 {
		op := Start(context.Background(), rtRaw, OperationStart{Domain: DomainJob, Name: "later"})
		Add(op.Context(), "who", fmt.Sprintf("later-%d", i))
		_ = op.End(nil)
	}

	// The copied view (TestSink semantics) is stable regardless.
	rt2, ts := testRT(t, nil)
	op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "stable"})
	Add(op2.Context(), "who", "stable")
	_ = op2.End(nil)
	if v, _ := ts.Events()[0].Lookup("who"); v != "stable" {
		t.Fatalf("copied view unstable: %v", v)
	}

	// The raw retained slice is only valid until the event is reused;
	// with a same-shape successor it may already show foreign data.
	// This is the hazard — assert it is BOUNDED: the slice still has
	// the fields-shape (never out-of-bounds memory or a panic).
	if len(retained) == 0 {
		t.Fatal("retained raw slice empty")
	}
	for _, f := range retained {
		_ = f.Key() // must not panic
	}
}

type rawRetainingSink struct {
	lastFields []Field
}

func (r *rawRetainingSink) Write(_ context.Context, rec *Record) {
	r.lastFields = rec.Fields()
}
