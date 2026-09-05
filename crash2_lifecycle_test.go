package hc

// Agent I — lifecycle and context misuse: deep operation chains on a
// shared context, concurrent Ends with different error pointers, stale
// contexts as parents, and end-order permutations.

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// 50 nested operations sharing the chain of contexts; ends run in
// reverse order — every event must land with its own fields.
func TestCrashDeepOperationChain(t *testing.T) {
	rt, ts := testRT(t, nil)
	const depth = 50
	ops := make([]*Operation, depth)
	ctx := context.Background()
	for i := range depth {
		ops[i] = Start(ctx, rt, OperationStart{Domain: DomainJob, Name: fmt.Sprintf("op%d", i)})
		ctx = ops[i].Context()
		Add(ctx, "depth", i)
	}
	for i := depth - 1; i >= 0; i-- {
		if !ops[i].End(nil) {
			t.Fatalf("op%d dropped", i)
		}
	}
	evs := ts.Events()
	if len(evs) != depth {
		t.Fatalf("events = %d, want %d", len(evs), depth)
	}
	seen := map[string]int{}
	for _, ev := range evs {
		name, _ := ev.Lookup("op.name")
		seen[name.(string)]++
	}
	for i := range depth {
		if seen[fmt.Sprintf("op%d", i)] != 1 {
			t.Fatalf("op%d seen %d times", i, seen[fmt.Sprintf("op%d", i)])
		}
	}
}

// Ends in scrambled order: an inner op may outlive its parent's End —
// the parent's seal does not gate the child (separate events).
func TestCrashScrambledEndOrder(t *testing.T) {
	rt, ts := testRT(t, nil)
	outer := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	inner := Start(outer.Context(), rt, OperationStart{Domain: DomainJob, Name: "job"})
	Add(inner.Context(), "inner", 1)

	_ = outer.End(nil)                    // parent commits first
	Add(outer.Context(), "late-outer", 1) // stale: no-op
	if !inner.End(nil) {
		t.Fatal("inner dropped after parent end")
	}
	Add(inner.Context(), "late-inner", 1) // stale: no-op

	if got := len(ts.Events()); got != 2 {
		t.Fatalf("events = %d, want 2", got)
	}
	for _, ev := range ts.Events() {
		if _, ok := ev.Lookup("late-outer"); ok {
			t.Fatal("late-outer write landed")
		}
		if _, ok := ev.Lookup("late-inner"); ok {
			t.Fatal("late-inner write landed")
		}
	}
}

// Concurrent first-End callers each hand a DIFFERENT error pointer:
// the winner's error is the one recorded — exactly once.
func TestCrashConcurrentEndDistinctErrPtrs(t *testing.T) {
	for attempt := range 20 {
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
		errs := make([]*error, 8)
		for i := range errs {
			e := fmt.Errorf("err-%d", i)
			errs[i] = &e
		}
		var wg sync.WaitGroup
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = op.End(errs[i])
			}(i)
		}
		wg.Wait()
		evs := ts.Events()
		if len(evs) != 1 {
			t.Fatalf("attempt %d: events = %d, want 1", attempt, len(evs))
		}
		count := 0
		for _, f := range evs[0].Fields() {
			if f.key == "error" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("attempt %d: error fields = %d, want 1", attempt, count)
		}
	}
}

// A finished operation's context used as the parent for a fresh
// request: the new request gets its own WAL; writes through the old
// ctx stay no-ops.
func TestCrashStaleCtxAsParent(t *testing.T) {
	rt, ts := testRT(t, nil)
	first := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	Add(first.Context(), "first", 1)
	_ = first.End(nil)

	second := Start(first.Context(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	Add(second.Context(), "second", 2)
	Add(first.Context(), "ghost", 3) // stale ctx write
	_ = second.End(nil)

	for _, ev := range ts.Events() {
		if _, ok := ev.Lookup("ghost"); ok {
			t.Fatal("ghost field leaked into an event")
		}
	}
	if len(ts.Events()) != 2 {
		t.Fatalf("events = %d, want 2", len(ts.Events()))
	}
}

// Start after Start on the same base ctx: both valid, independent.
func TestCrashSiblingOpsSameBase(t *testing.T) {
	rt, ts := testRT(t, nil)
	base := context.Background()
	a := Start(base, rt, OperationStart{Domain: DomainCLI, Name: "a"})
	b := Start(base, rt, OperationStart{Domain: DomainCLI, Name: "b"})
	Add(a.Context(), "who", "a")
	Add(b.Context(), "who", "b")
	_ = a.End(nil)
	_ = b.End(nil)
	evs := ts.Events()
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2", len(evs))
	}
	whos := map[string]bool{}
	for _, ev := range evs {
		v, _ := ev.Lookup("who")
		whos[v.(string)] = true
	}
	if !whos["a"] || !whos["b"] {
		t.Fatalf("siblings crossed: %v", whos)
	}
}

// Start on an already-canceled context: the operation must still run,
// commit, and emit — the WAL is ctx-independent (cancellation surfaces
// through the caller's error, not the context).
func TestCrashStartOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt, ts := testRT(t, nil)
	op := Start(ctx, rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "k", "v")
	if !op.End(nil) || len(ts.Events()) != 1 {
		t.Fatal("canceled parent context broke the lifecycle")
	}
}

// AddRawJSON keeps the caller's []byte by reference: mutating the blob
// between Add and End is visible on the wire (torn JSON, silently).
// Pin the lifetime contract: encode before you reuse the buffer.
func TestCrashRawJSONBlobLifetime(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	blob := []byte(`{"v":1}`)
	AddRawJSON(op.Context(), "raw", blob)
	blob[5] = '9' // caller mutates before End
	_ = op.End(nil)

	ev := ts.Events()[0]
	raw, ok := ev.Lookup("raw")
	if !ok {
		t.Fatal("raw field missing")
	}
	b, _ := raw.([]byte)
	if string(b) != `{"v":9}` {
		t.Fatalf("raw blob was copied at Add time: %q (lifetime contract is by-reference)", b)
	}
}
