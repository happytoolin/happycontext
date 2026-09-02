package hc

import (
	"context"
	"errors"
	"runtime/debug"
	"testing"
)

// TestPoolReuseCanary pins the WAL pool-recycle contract with the
// sentinel technique from log/slog's TestAliasingAndClone
// (record_test.go): slog inserts a "!BUG" attr wherever an unsafe
// shared-backing write would land, so the violation fails loudly with
// the broken contract named instead of silently corrupting data. Our
// adaptation (dst-research §11.3): the pooled buffer is the *event
// (fields backing slice included) that End releases to eventPool and
// the next request reuses; the sentinel is a canary field written
// through a stale (already-ended) context. A correct implementation
// drops every one of those writes at the generation check (wal.go
// amendments 1/20); any write that lands is a use-after-recycle.
//
// zap's internal/pool/pool_test.go contributes the other half: the GC
// and (under -race) the runtime both drain sync.Pool, so the critical
// window runs with GC disabled to keep the pool hot. Recycle is NOT
// asserted — the race runtime drops a share of Puts entirely (~20-30%
// of runs never see the exact event again), so the test fires its
// sentinel writes across a churn of requests and guards whichever
// events the pool hands out; the deterministic reset-path half lives
// in TestWALAppendSealedStaleGeneration.
func TestPoolReuseCanary(t *testing.T) {
	oldGC := debug.SetGCPercent(-1) // zap: keep the pool hot for the whole test
	defer debug.SetGCPercent(oldGC)

	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: 1})

	// Request 1 writes real payload, then ends: the event is sealed and
	// returned to the pool, and ctx1 is now stale — it still pins the
	// pooled *event.
	op1 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "one"})
	ctx1 := op1.Context()
	Add(ctx1, "payload", "request-one")
	op1.End(nil)
	ev1 := op1.ev

	// Chase the recycled event across requests. GC off keeps the pool
	// hot, but the race runtime still drops a share of sync.Pool Puts
	// (~20-30% of runs never see the exact event again; zap's
	// internal/pool note), so observation is best-effort: every round
	// fires the stale writes against whatever event the pool returned
	// (recycled or fresh — both must reject them), and the reset-path
	// generation bump is deterministically covered by
	// TestWALAppendSealedStaleGeneration. A round that gets op1's event
	// back additionally guards the write-after-reset window.
	ctx2 := context.Background()
	recycled := false
	rounds := 0
	for rounds < 16 && !recycled {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "churn"})
		ctx := op.Context()
		ctx2 = ctx
		rounds++
		Add(ctx, "payload", "churn")
		canaryWrite(ctx1) // live window: stale gen must fail every entry point
		op.End(nil)
		canaryWrite(ctx1) // pooled window: stale gen must fail after release too
		recycled = op.ev == ev1
	}
	canaryWrite(ctx2) // last request's context is stale after its End as well

	// The oracle: no captured event — not request 1's, not any recycled
	// request's — may ever contain the sentinel. The assertion message
	// names the contract so a regression report points at the fix, in
	// slog's "!BUG" spirit.
	for i, ev := range ts.Events() {
		if v, ok := ev.Lookup("!BUG"); ok {
			t.Fatalf("event %d: use-after-recycle — a write through a stale "+
				"context landed on a pooled event (slog !BUG canary): %v; "+
				"the generation check in append/setMessage/setLevel "+
				"(wal.go amendments 1/20) must reject post-End writes", i, v)
		}
	}
	events := ts.Events()
	if got := len(events); got != 1+rounds {
		t.Fatalf("captured %d events, want %d (1 + %d churn rounds)", got, 1+rounds, rounds)
	}
	if v, _ := events[0].Lookup("payload"); v != "request-one" {
		t.Fatalf("event 0 payload = %v, want %q", v, "request-one")
	}
	for i := 1; i < len(events); i++ {
		if v, _ := events[i].Lookup("payload"); v != "churn" {
			t.Fatalf("churn event %d payload = %v, want %q", i, v, "churn")
		}
	}
}

// canaryWrite replays every public write shape through a stale context.
// The sentinel key and value are chosen so their presence on any wire
// is unambiguous (slog renders the same idea as String("!BUG", …)).
func canaryWrite(stale context.Context) {
	Add(stale, "!BUG", "stale write through a released context")
	SetMessage(stale, "!BUG stale message")
	SetLevel(stale, LevelError)
	Error(stale, errors.New("!BUG stale error"))
}
