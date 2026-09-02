package hc

// P5 concurrent-End characterization: End is documented request-
// confined ("exactly one goroutine drives it"), but concurrent misuse
// must be characterized and safe. With the one-shot claim word
// (operation.go endState): exactly one caller CASes 0→1 and commits;
// every other caller waits for publication and returns the winner's
// cached result. These tests pin: single emission, consistent return
// values, no data race (run under -race), and a clean pool after the
// race. Sequential double-End behavior is pinned by
// TestLifecycleOneShotEnd.

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
)

// concurrentEnd races n goroutines over one End on a fresh runtime and
// returns their results plus the emitted event count.
func concurrentEnd(t *testing.T, rate float64, n int) ([]bool, int) {
	t.Helper()
	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: rate})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "race"})

	results := make([]bool, n)
	var mu sync.Mutex
	start := sync.NewCond(&mu)
	released, ready := false, 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mu.Lock()
			ready++
			start.Broadcast()
			for !released {
				start.Wait()
			}
			mu.Unlock()
			results[i] = op.End(nil)
		}(i)
	}
	mu.Lock()
	for ready != n {
		start.Wait()
	}
	released = true
	start.Broadcast()
	mu.Unlock()
	wg.Wait()

	return results, len(ts.Events())
}

// TestConcurrentEndCharacterization races End on one operation: the
// winner commits exactly one event and every caller observes the same
// emitted result (the losers wait for the winner's publication).
func TestConcurrentEndCharacterization(t *testing.T) {
	// Emitted case (rate 1): exactly one event, every caller sees true.
	for round := 0; round < 25; round++ {
		results, events := concurrentEnd(t, 1, 16)
		if events != 1 {
			t.Fatalf("round %d: %d events, want exactly 1", round, events)
		}
		for i, r := range results {
			if !r {
				t.Fatalf("round %d: caller %d returned false although the event was emitted", round, i)
			}
		}
	}
	// Sampled-away case (rate 0): no event, every caller sees false.
	for round := 0; round < 25; round++ {
		results, events := concurrentEnd(t, 0, 16)
		if events != 0 {
			t.Fatalf("round %d: %d events at rate 0", round, events)
		}
		for i, r := range results {
			if r {
				t.Fatalf("round %d: caller %d saw an emission that never happened", round, i)
			}
		}
	}
}

// TestConcurrentEndArmed races End on an armed event: the armed-seal
// mutex and the claim word must keep the whole race single-emission
// and race-free (watchdog-style guarded appends interleave with the
// End callers).
func TestConcurrentEndArmed(t *testing.T) {
	for round := 0; round < 25; round++ {
		ts := NewTestSink()
		rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "armed-race"})
		ctx := op.Context()
		op.ev.arm()

		var mu sync.Mutex
		start := sync.NewCond(&mu)
		released, ready := false, 0
		const n = 8
		var wg sync.WaitGroup
		results := make([]bool, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				mu.Lock()
				ready++
				start.Broadcast()
				for !released {
					start.Wait()
				}
				mu.Unlock()
				if i%2 == 0 {
					results[i] = op.End(nil)
				} else {
					Add(ctx, "async", i) // guarded append racing the seal
					results[i] = false
				}
			}(i)
		}
		mu.Lock()
		for ready != n {
			start.Wait()
		}
		released = true
		start.Broadcast()
		mu.Unlock()
		wg.Wait()

		if got := len(ts.Events()); got != 1 {
			t.Fatalf("round %d: %d events, want exactly 1", round, got)
		}
		for i, r := range results {
			if r != (i%2 == 0) {
				t.Fatalf("round %d: caller %d returned %v", round, i, r)
			}
		}
		// Pool is clean for the next request.
		ok := &matrixSink{}
		rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
		op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
		Add(op2.Context(), "clean", true)
		op2.End(nil)
		if len(ok.capture) != 1 || !bytes.Contains(ok.capture[0], []byte(`"clean":true`)) {
			t.Fatalf("round %d: pool corrupted after the armed race", round)
		}
	}
}

// TestConcurrentEndErrorPointer races End with per-caller error
// pointers: the winner's error is the one recorded; the event commits
// exactly once and every caller sees the same result.
func TestConcurrentEndErrorPointer(t *testing.T) {
	for round := 0; round < 25; round++ {
		ts := NewTestSink()
		rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "err-race"})
		results := make([]bool, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				err := fmt.Errorf("caller-%d", i)
				results[i] = op.End(&err)
			}(i)
		}
		wg.Wait()
		if len(ts.Events()) != 1 {
			t.Fatalf("round %d: %d events", round, len(ts.Events()))
		}
		for i := 1; i < len(results); i++ {
			if results[i] != results[0] {
				t.Fatalf("round %d: inconsistent results %v", round, results)
			}
		}
	}
}
