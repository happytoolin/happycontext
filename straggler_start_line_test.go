package hc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestStragglerStartLine stresses the straggler-vs-recycle window with
// logrus's start-line technique (logrus_test.go,
// TestLoggingRaceWithHooksOnEntry — sync.NewCond + Broadcast): every
// goroutine is held on a sync.Cond and released simultaneously with
// one Broadcast, so stale writes, owner writes, and End/seal/recycle
// all contend on the same instruction window instead of staggered
// goroutine spawns — the shape that maximizes race-window hits
// (dst-research §11.4).
//
// Cast: one owner churns Start/Add/End requests so the pooled event a
// stale context pins is constantly reset and reused, while straggler
// goroutines replay every write shape through that stale context. The
// oracle is deterministic-in-aggregate (logrus's assertion shape —
// counts and per-event consistency, never ordering): exactly one event
// per request, each carrying its own round marker, none ever containing
// a straggler write. Run under -race this pins the amendment-1/20
// protocol: a stale write can never land on a live or recycled event.
func TestStragglerStartLine(t *testing.T) {
	const (
		stragglers = 6
		rounds     = 400
	)

	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: 1})

	// The shared stale context: op0 ends before the race starts, its
	// event is pooled, and the owner's churn below recycles it
	// continuously while the stragglers write into it.
	op0 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "stale"})
	stale := op0.Context()
	op0.End(nil)

	var stragglerWrites atomic.Int64 // sanity: the stragglers must actually run

	var mu sync.Mutex
	start := sync.NewCond(&mu)
	released := false
	registered := 0
	workers := stragglers + 1 // stragglers + owner

	release := func() {
		mu.Lock()
		defer mu.Unlock()
		for registered != workers {
			start.Wait() // go to sleep; a worker's Broadcast wakes us
		}
		released = true
		start.Broadcast()
	}

	var wg sync.WaitGroup
	enter := func() {
		mu.Lock()
		registered++
		start.Broadcast() // wake the releaser if it is already waiting
		for !released {
			start.Wait()
		}
		mu.Unlock()
	}

	wg.Add(workers + 1) // owner + stragglers + releaser
	go func() {         // releaser: fires the start line exactly once
		defer wg.Done()
		release()
	}()

	// Owner: churns requests; each round's event is (usually) the same
	// pooled one op0 released, so the stale writes below constantly hit
	// a live-then-recycled target.
	go func() {
		defer wg.Done()
		enter()
		for i := 0; i < rounds; i++ {
			op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "churn"})
			Add(op.Context(), "owner", i)
			op.End(nil)
		}
	}()

	// Stragglers: replay the full stale-write vocabulary against the
	// recycled event, released at the same instant as the owner.
	for s := 0; s < stragglers; s++ {
		go func(s int) {
			defer wg.Done()
			enter()
			for i := 0; i < rounds; i++ {
				key := fmt.Sprintf("!BUG-straggler-%d", s)
				Add(stale, key, i)
				if i%3 == 0 {
					SetMessage(stale, "!BUG stale message")
					SetLevel(stale, LevelError)
				}
				if i%7 == 0 {
					Error(stale, errors.New("!BUG stale error"))
				}
				stragglerWrites.Add(1)
			}
		}(s)
	}
	wg.Wait()

	if got := stragglerWrites.Load(); got != stragglers*rounds {
		t.Fatalf("stragglers executed %d writes, want %d", got, stragglers*rounds)
	}

	events := ts.Events()
	if len(events) != 1+rounds { // op0 + one per owner round
		t.Fatalf("captured %d events, want %d", len(events), 1+rounds)
	}
	// Per-event integrity: every round's event carries exactly its own
	// marker — the request-confined payload a torn recycle would corrupt
	// — and no event (not even op0's) contains any straggler write.
	for i, ev := range events {
		if i == 0 {
			continue // op0 predates the race; checked by the scan below
		}
		want := i - 1
		if v, ok := ev.Lookup("owner"); !ok || v.(int64) != int64(want) {
			t.Fatalf("event %d: owner marker = %v (ok=%v), want %d — a stale "+
				"write corrupted a live event", i, v, ok, want)
		}
	}
	for i, ev := range events {
		for s := 0; s < stragglers; s++ {
			if _, ok := ev.Lookup(fmt.Sprintf("!BUG-straggler-%d", s)); ok {
				t.Fatalf("event %d contains a straggler write — stale context "+
					"mutated a sealed/recycled event (amendment 20)", i)
			}
		}
	}
}
