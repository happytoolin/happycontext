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
// window runs with GC disabled — that both keeps the pool hot and makes
// Put-then-Get return the same event deterministically, which the test
// asserts so it can never pass vacuously.
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

	// Recycle must be deterministic with GC off (same-goroutine Put then
	// Get hits the pool's private slot): if the pool ever stops reusing
	// the event, the canary below guards nothing and the test must say
	// so instead of passing silently.
	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "two"})
	ev2 := op2.ev
	if ev2 != ev1 {
		t.Fatal("pool did not recycle the released event; canary guards nothing")
	}
	ctx2 := op2.Context()
	Add(ctx2, "payload", "request-two")

	// Canary volley 1 — while request 2 is LIVE on the recycled event.
	// Each write must no-op: the stale generation must fail every entry
	// point (append / setMessage / setLevel / setError).
	canaryWrite(ctx1)
	op2.End(nil)

	// Canary volley 2 — after request 2 ended, the event is pooled again
	// and immediately reused by request 3; the same stale context keeps
	// firing across the churn.
	for i := 0; i < 32; i++ {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "churn"})
		Add(op.Context(), "payload", "churn")
		canaryWrite(ctx1) // live window
		op.End(nil)
		canaryWrite(ctx1) // pooled window
	}
	canaryWrite(ctx2) // ctx2 is stale after request 2 too

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
	if got := len(events); got != 34 {
		t.Fatalf("captured %d events, want 34 (1 + 1 + 32 churn)", got)
	}
	for i, want := range []string{"request-one", "request-two"} {
		if v, _ := events[i].Lookup("payload"); v != want {
			t.Fatalf("event %d payload = %v, want %q", i, v, want)
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
