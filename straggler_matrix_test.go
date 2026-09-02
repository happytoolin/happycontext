package hc

// P5 straggler-injection matrix (action plan P5, dst-research §5/§8):
// an instrumented runner fires straggler writes at every phase of End
// and verifies the committed record is untouched, the event's sealed
// state is correct, and the pool stays clean for subsequent requests.
//
// End's owner critical section (annotate → seal → scan → post-seal →
// commit → sink.Write → release) has no externally callable seams, so
// the matrix drives a STAGED End: the same internal steps in the same
// order the real End runs, with a fire hook at each boundary. The real
// End's ordering is pinned separately by the T2 lifecycle model
// oracle; this file pins the straggler invariants at each boundary.
// Everything is deterministic — one goroutine, no sleeps.
//
// Semantics per phase:
//   - pre-seal phases: straggler writes are legally in-flight (they
//     land and become part of the record, exactly as if the owner had
//     written them — the record must equal that control run).
//   - post-seal phases: every mutation type must no-op; the record
//     must equal the no-straggler control run.
//   - in-write: the sink fires the straggler before reading the record
//     (the amendment-20 threat-model window).
//   - post-commit / post-release / post-recycle: stale writes through
//     the request context must never corrupt the pool or a later
//     request.
//
// Armed events serialize appends under the per-event mutex, so every
// phase also runs armed; TestStragglerArmedBurst exercises the mutex
// discipline concurrently under -race.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// matrixPhase enumerates the staged-End boundaries.
type matrixPhase int

const (
	phasePreAnnotate          matrixPhase = iota // before annotateOperationFailures (live)
	phasePreSeal                                 // after annotations, before seal (live)
	phasePreScan                                 // after seal, before scanWAL
	phasePrePostSeal                             // after scan/resolve, before annotatePostSeal
	phasePreCommit                               // after annotatePostSeal, before commit
	phaseInWrite                                 // inside sink.Write (fired by the sink)
	phasePostCommitPreRelease                    // after commit, before release
	phasePostRelease                             // after release (event back in the pool)
	phasePostRecycle                             // after a second request recycled the pool
)

func (p matrixPhase) String() string {
	names := [...]string{
		"pre-annotate", "pre-seal", "pre-scan", "pre-postseal", "pre-commit",
		"in-write", "post-commit-pre-release", "post-release", "post-recycle",
	}
	return names[p]
}

// live reports whether the phase precedes seal (straggler writes land).
func (p matrixPhase) live() bool { return p == phasePreAnnotate || p == phasePreSeal }

// stragglerWrite fires all six public mutation shapes through ctx —
// the full straggler vocabulary the matrix applies at every phase.
func stragglerWrite(ctx context.Context) {
	// SetMessage/SetLevel fire FIRST: they are the shapes whose armed
	// serialization under the event mutex is the review-pinned
	// discipline (TestArmedSetterSerialization), so putting them first
	// widens the race window the concurrent tests need.
	SetMessage(ctx, "s-message")
	SetLevel(ctx, LevelError)
	Add(ctx, "s-add", "straggler-value")
	AddRawJSON(ctx, "s-raw", []byte(`{"s":true}`))
	Error(ctx, errors.New("s-error"))
	SetRoute(ctx, "/s-route")
}

// matrixSink captures committed records and can fire a callback during
// Write (the in-write phase). The callback runs BEFORE the record is
// read, so a landed straggler write would be visible in the capture.
type matrixSink struct {
	fire    func()
	capture [][]byte
}

func (s *matrixSink) Write(_ context.Context, rec *Record) {
	if s.fire != nil {
		s.fire()
	}
	s.capture = append(s.capture, bytes.Clone(rec.Encoded()))
}

// stagedEnd runs the internal steps of End in order, firing the
// straggler at the phase boundary, then commits through the real
// op.commit (which reaches the sink). fireAt == -1 disables the
// boundary fire (used when the sink fires instead). The caller
// releases the event when it wants the pool phases.
func stagedEnd(op *Operation, fireAt matrixPhase, fire func(ctx context.Context)) bool {
	ev := op.ev
	rt := op.rt
	start := op.start

	now := time.Now()
	duration := now.Sub(ev.startedAt)

	if fireAt == phasePreAnnotate {
		fire(op.Context())
	}
	annotateOperationFailures(ev, &op.ref, nil, nil)
	if fireAt == phasePreSeal {
		fire(op.Context())
	}
	ev.seal()
	if fireAt == phasePreScan {
		fire(op.Context())
	}
	scan := scanWAL(ev)
	code := scan.code
	outcome := resolveOutcomeV2(nil, nil, code, scan.outcome)
	if fireAt == phasePrePostSeal {
		fire(op.Context())
	}
	annotatePostSeal(ev, &op.ref, duration, normalizeDomain(start.Domain) == DomainHTTP, scan, outcome)
	if fireAt == phasePreCommit {
		fire(op.Context())
	}
	return op.commit(ev, rt, start, outcome, code, duration, now, nil, false, scan)
}

// TestStragglerInjectionMatrix drives every phase in both armed modes
// and compares the committed record against its control (the same
// writes as owner writes for live phases, no writes for sealed ones).
func TestStragglerInjectionMatrix(t *testing.T) {
	phases := []matrixPhase{
		phasePreAnnotate, phasePreSeal, phasePreScan, phasePrePostSeal,
		phasePreCommit, phaseInWrite, phasePostCommitPreRelease,
		phasePostRelease, phasePostRecycle,
	}
	for _, phase := range phases {
		for _, armed := range []bool{false, true} {
			phase, armed := phase, armed
			t.Run(fmt.Sprintf("phase=%s armed=%v", phase, armed), func(t *testing.T) {
				live := phase.live()

				// Control run: identical setup, straggler writes made by
				// the owner (live) or not at all (sealed), ended for real.
				controlSink := &matrixSink{}
				cop := Start(context.Background(),
					MustCompile(Config{Sink: controlSink, SamplingRate: 1}),
					OperationStart{Domain: DomainJob, Name: "m"})
				if armed {
					cop.ev.arm()
				}
				if live {
					stragglerWrite(cop.Context())
				}
				cop.End(nil)
				if len(controlSink.capture) != 1 {
					t.Fatalf("control captured %d events", len(controlSink.capture))
				}
				control := controlSink.capture[0]

				// Matrix run.
				sink := &matrixSink{}
				rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "m"})
				ctx := op.Context()
				if armed {
					op.ev.arm()
				}
				// The state assertion runs at the phase boundary, right
				// after the straggler fires: live phases must still be
				// live/armed, sealed phases must be sealed.
				checkState := func(wantLive bool) {
					state := walState(op.ev.state.Load() & walStateMask)
					if wantLive {
						if state != walLive && state != walArmed {
							t.Fatalf("phase %v: state %v, want live/armed", phase, state)
						}
					} else if state != walSealed && state != walSealedArmed {
						t.Fatalf("phase %v: state %v, want sealed", phase, state)
					}
				}
				fire := func(context.Context) {
					stragglerWrite(ctx)
					checkState(phase.live())
				}

				switch phase {
				case phaseInWrite:
					// The sink fires inside commit's emit: post-seal.
					sink.fire = func() { fire(ctx) }
					stagedEnd(op, -1, fire)
				case phasePostCommitPreRelease:
					stagedEnd(op, -1, fire)
					fire(ctx)
					op.ev.release()
				case phasePostRelease:
					stagedEnd(op, -1, fire)
					op.ev.release()
					fire(ctx)
				case phasePostRecycle:
					stagedEnd(op, -1, fire)
					op.ev.release()
					op2 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "two"})
					Add(op2.Context(), "own", "field")
					op2.End(nil)
					fire(ctx) // stale writes must drop everywhere
				default:
					stagedEnd(op, phase, fire)
				}

				// After the full staged End the event is sealed in every
				// mode.
				state := walState(op.ev.state.Load() & walStateMask)
				if state != walSealed && state != walSealedArmed {
					t.Fatalf("phase %v: final state %v, want sealed", phase, state)
				}

				label := fmt.Sprintf("phase=%s armed=%v", phase, armed)
				if phase == phasePostRecycle {
					// Two captures: the matrix event and the second
					// request's own event. The first must equal the
					// control; the second must carry only its own data.
					if len(sink.capture) != 2 {
						t.Fatalf("%s: captured %d events, want 2", label, len(sink.capture))
					}
					compareLifecycleLines(t, control, sink.capture[0], label)
					if !bytes.Contains(sink.capture[1], []byte(`"own":"field"`)) {
						t.Fatalf("%s: second event lost its own field: %s", label, sink.capture[1])
					}
					if bytes.Contains(sink.capture[1], []byte(`"s-add"`)) {
						t.Fatalf("%s: straggler leaked into the second event: %s", label, sink.capture[1])
					}
					return
				}
				if len(sink.capture) != 1 {
					t.Fatalf("%s: captured %d events, want 1", label, len(sink.capture))
				}
				compareLifecycleLines(t, control, sink.capture[0], label)
			})
		}
	}
}

// TestStragglerPoolIntegrity is the matrix's pool phase: after the
// straggler rounds, subsequent requests emit exactly their own fields.
func TestStragglerPoolIntegrity(t *testing.T) {
	sink := &matrixSink{}
	rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
	// churn a few requests so the pool recycles
	for i := 0; i < 8; i++ {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "churn"})
		stragglerWrite(op.Context()) // pre-seal: lands (owner-equivalent)
		op.End(nil)
		stragglerWrite(op.Context()) // post-End: must drop
	}
	if len(sink.capture) != 8 {
		t.Fatalf("captured %d events, want 8", len(sink.capture))
	}
	for i, line := range sink.capture {
		if !bytes.Contains(line, []byte(`"s-add":"straggler-value"`)) {
			t.Fatalf("event %d lost its pre-seal write: %s", i, line)
		}
		// post-End straggler writes and the s-raw/s-error/s-route fields
		// are all pre-seal lands here; only verify structural sanity on
		// the line plus no dup keys.
		if !bytes.Contains(line, []byte(`"op.outcome":"success"`)) {
			t.Fatalf("event %d malformed: %s", i, line)
		}
	}
	// every event decodes and has exactly one s-add member
	_ = verifyPoolCleanLine(t, sink.capture)
}

// compareLifecycleLines compares two encoded lines member by member
// after dropping the wall-clock members (time, duration_ms) — the same
// exclusion the T2 lifecycle oracle uses.
func compareLifecycleLines(t *testing.T, want, got []byte, label string) {
	t.Helper()
	_, wantMembers := decodeLineStrict(t, want)
	_, gotMembers := decodeLineStrict(t, got)
	filter := func(members []rawMember) []rawMember {
		out := make([]rawMember, 0, len(members))
		for _, m := range members {
			if m.key == "time" || m.key == "duration_ms" {
				continue
			}
			out = append(out, m)
		}
		return out
	}
	wantMembers, gotMembers = filter(wantMembers), filter(gotMembers)
	if len(wantMembers) != len(gotMembers) {
		t.Fatalf("%s: member count %d != control %d\n got  %v\n want %v",
			label, len(gotMembers), len(wantMembers), memberKeys(gotMembers), memberKeys(wantMembers))
	}
	for i := range wantMembers {
		if wantMembers[i].key != gotMembers[i].key || !bytes.Equal(wantMembers[i].val, gotMembers[i].val) {
			t.Fatalf("%s: member %d differs:\n got  %s:%s\n want %s:%s",
				label, i, gotMembers[i].key, gotMembers[i].val, wantMembers[i].key, wantMembers[i].val)
		}
	}
}

// verifyPoolCleanLine asserts each line decodes with one event shape.
func verifyPoolCleanLine(t *testing.T, lines [][]byte) bool {
	t.Helper()
	for _, line := range lines {
		_, members := decodeLineStrict(t, line)
		count := 0
		for _, m := range members {
			if m.key == "s-add" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("s-add appears %d times in %s", count, line)
		}
	}
	return true
}

// TestStragglerArmedBurst releases straggler goroutines on an armed
// event while the owner commits: the per-event mutex serializes every
// append, so under -race this pins the armed-seal discipline. The
// assertions are interleaving-tolerant (each write either lands
// pre-seal or drops post-seal; never a torn state).
func TestStragglerArmedBurst(t *testing.T) {
	for round := 0; round < 50; round++ {
		sink := &matrixSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "burst"})
		ctx := op.Context()
		op.ev.arm()

		var mu sync.Mutex
		start := sync.NewCond(&mu)
		released, ready := false, 0
		const stragglers = 4
		var wg sync.WaitGroup
		for i := 0; i < stragglers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mu.Lock()
				ready++
				start.Broadcast()
				for !released {
					start.Wait()
				}
				mu.Unlock()
				stragglerWrite(ctx) // armed: serialized under ev.mu
			}()
		}
		// release all stragglers at once (logrus start-line technique)
		mu.Lock()
		for ready != stragglers {
			start.Wait()
		}
		released = true
		start.Broadcast()
		mu.Unlock()

		// The owner ends while the stragglers are mid-flight: guarded
		// appends land only while the event is armed (pre-seal).
		op.End(nil)
		wg.Wait()

		if len(sink.capture) != 1 {
			t.Fatalf("round %d: captured %d events", round, len(sink.capture))
		}
		// The record must be valid and internally consistent: whatever
		// landed pre-seal is present exactly once per key, nothing is
		// torn, and no straggler key appears twice.
		_, members := decodeLineStrict(t, sink.capture[0])
		seen := map[string]bool{}
		for _, m := range members {
			if seen[m.key] {
				t.Fatalf("round %d: duplicate member %q: %s", round, m.key, sink.capture[0])
			}
			seen[m.key] = true
		}
	}
}

// TestStragglerSealedErrorNoLatch pins the hasErr-latch half of the
// live() narrowing (review finding GLM-1): an Error straggler fired at
// a sealed phase must neither append the error field NOR latch hasErr.
// The latch is only observable through sampling — a latched hasErr
// flips a rate-0 healthy event into the amendment-4 bypass and emits
// it — so the matrix runs at rate 0: zero captures is the oracle. The
// hasErr field is asserted directly as well.
func TestStragglerSealedErrorNoLatch(t *testing.T) {
	for _, armed := range []bool{false, true} {
		for _, phase := range []matrixPhase{phasePreScan, phasePrePostSeal, phasePreCommit} {
			phase, armed := phase, armed
			t.Run(fmt.Sprintf("phase=%s armed=%v", phase, armed), func(t *testing.T) {
				sink := &matrixSink{}
				rt := MustCompile(Config{Sink: sink, SamplingRate: 0}) // healthy drops
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "latch"})
				ctx := op.Context()
				if armed {
					op.ev.arm()
				}
				stagedEnd(op, phase, func(context.Context) {
					Error(ctx, errors.New("s-error"))
				})
				if op.ev.hasErr {
					t.Fatalf("phase %v armed=%v: hasErr latched by a sealed-phase straggler", phase, armed)
				}
				if len(sink.capture) != 0 {
					t.Fatalf("phase %v armed=%v: %d events at rate 0 — the latched "+
						"hasErr bypassed sampling", phase, armed, len(sink.capture))
				}
			})
		}
	}
}

// TestArmedSetterSerialization is the deterministic companion to the
// -race burst: concurrent SetMessage/SetLevel calls on an armed event
// must serialize under the event mutex. A regression dropping the mu
// is a plain data race, so this test's detection power is the -race
// window — it widens that window by hammering ONLY the setter shapes
// (the ones the fix serialized) across many rounds while the owner
// seals. The assertions (single event, sane message) hold under both
// the fixed and the reverted code; the race detector is the oracle.
func TestArmedSetterSerialization(t *testing.T) {
	for round := 0; round < 40; round++ {
		sink := &matrixSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "setter-race"})
		ctx := op.Context()
		op.ev.arm()

		const setters = 6
		const writes = 2000
		var wg sync.WaitGroup
		for i := 0; i < setters; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				for j := 0; j < writes; j++ {
					SetMessage(ctx, fmt.Sprintf("msg-%d", i))
					SetLevel(ctx, LevelWarn)
				}
			}(i)
		}
		// The owner seals while the setters hammer: armed serialization
		// means every write either lands pre-seal or drops post-seal.
		op.End(nil)
		wg.Wait()

		if len(sink.capture) != 1 {
			t.Fatalf("round %d: %d events", round, len(sink.capture))
		}
		if _, err := decodeLineStrict(t, sink.capture[0]); err == nil && len(sink.capture[0]) == 0 {
			t.Fatalf("round %d: empty line", round)
		}
	}
}
