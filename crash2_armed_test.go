package hc

// Agent L — armed-mode deterministic stress. The armed protocol
// (guarded appends under the event mutex) exists for concurrent
// writers and the future watchdog: hammer it with mixed writers,
// setters, snapshots, arm-during-commit races, and a single End.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Mixed writers on one ARMED event plus one End: no race (pinned by
// -race), exactly one event, line valid.
func TestCrashArmedMixedWritersSingleEnd(t *testing.T) {
	for round := range 10 {
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
		op.ev.arm()

		var wg sync.WaitGroup
		stop := make(chan struct{})

		for w := range 6 {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					Add(op.Context(), fmt.Sprintf("w%d", w), i)
				}
			}(w)
		}
		for s := range 3 {
			wg.Add(1)
			go func(s int) {
				defer wg.Done()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					switch s % 3 {
					case 0:
						Error(op.Context(), fmt.Errorf("soft-%d", i))
					case 1:
						SetLevel(op.Context(), LevelWarn)
					case 2:
						SetMessage(op.Context(), fmt.Sprintf("m-%d", i))
					}
				}
			}(s)
		}
		// Watchdog-style snapshotter.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = op.ev.snapshotFields()
				}
			}
		}()

		_ = op.End(nil)
		close(stop)
		wg.Wait()

		evs := ts.Events()
		if len(evs) != 1 {
			t.Fatalf("round %d: events = %d, want 1", round, len(evs))
		}
		rec := recOf(evs[0].Level(), evs[0].Message(), evs[0].Fields()...)
		if !json.Valid(rec.Encoded()) {
			t.Fatalf("round %d: line invalid", round)
		}
	}
}

// arm() racing End's seal: arm takes the mutex and only converts live
// events; seal-of-armed takes it too. Both interleavings must be safe.
func TestCrashArmRacingSeal(t *testing.T) {
	for round := range 20 {
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		Add(op.Context(), "k", round)

		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					op.ev.arm()
				}
			}
		}()
		_ = op.End(nil)
		close(stop)
		wg.Wait()

		if len(ts.Events()) != 1 {
			t.Fatalf("round %d: events = %d", round, len(ts.Events()))
		}
	}
}

// setError racing seal on an armed event: the P5 discipline (append +
// hasErr latch under the same mutex as the seal) must keep the latch
// consistent — an error that landed is never lost, a straggler error
// post-seal never latches.
func TestCrashArmedErrorVsSeal(t *testing.T) {
	for round := range 20 {
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		op.ev.arm()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			Error(op.Context(), fmt.Errorf("racing-%d", round))
		}()
		_ = op.End(nil)
		wg.Wait()

		evs := ts.Events()
		if len(evs) != 1 {
			t.Fatalf("round %d: events = %d", round, len(evs))
		}
		// With End(nil) the outcome can only be success: outcome derives
		// from the deferred error pointer, not the WAL error field, so a
		// racing Error() that lands pre-seal yields success WITH an error
		// field — a legitimate state. The oracle is the latch itself: if
		// an error field is present it must be the racing error, and a
		// post-seal Error() must never latch.
		o, _ := evs[0].Lookup("op.outcome")
		if o != string(OutcomeSuccess) {
			t.Fatalf("round %d: outcome = %v, want success (outcome is errp-derived)", round, o)
		}
		if errVal, hasErr := evs[0].Lookup("error"); hasErr {
			m, _ := errVal.(map[string]any)
			msg, _ := m["message"].(string)
			if msg != fmt.Sprintf("racing-%d", round) {
				t.Fatalf("round %d: foreign error field latched: %#v", round, errVal)
			}
		}
	}
}

// Snapshot racing the owner's post-seal appends (annotatePostSeal):
// snapshots are copies, so the committed line is unaffected.
func TestCrashSnapshotVsPostSealAppends(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	op.ev.arm()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = op.ev.snapshotFields()
			}
		}
	}()
	Add(op.Context(), "k", "v")
	_ = op.End(nil)
	close(stop)
	wg.Wait()

	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	rec := recOf(evs[0].Level(), evs[0].Message(), evs[0].Fields()...)
	line := string(rec.Encoded())
	// http.status is middleware-written (not core): core completion
	// fields only.
	for _, want := range []string{`"op.outcome"`, `"duration_ms"`, `"op.domain"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("completion fields missing (%s): %s", want, line)
		}
	}
}
