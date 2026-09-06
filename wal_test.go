package hc

// WAL protocol tests: the state machine and append paths, the loom-lite
// deterministic simulations of seal/recycle races, and the straggler
// injection matrix. One file because they verify one protocol.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWALAppendOrder(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	ev.addKV(ref, "a", 1, "b", "two", "c", true)

	if got := len(ev.fields); got != 3 {
		t.Fatalf("field count = %d", got)
	}
	wantKeys := []string{"a", "b", "c"}
	for i, want := range wantKeys {
		if ev.fields[i].key != want {
			t.Fatalf("fields[%d].key = %q, want %q", i, ev.fields[i].key, want)
		}
	}
}

func TestWALMalformedKVTailSkipped(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	// odd tail and non-string key are skipped; valid pairs survive
	ev.addKV(ref, "k", 1, "good", 2, 3, "bad", "", "also bad", "fine", 3)
	keys := map[string]bool{}
	for _, f := range ev.fields {
		keys[f.key] = true
	}
	for _, want := range []string{"k", "good", "fine"} {
		if !keys[want] {
			t.Errorf("missing key %q", want)
		}
	}
	if keys[""] || keys["3"] {
		t.Errorf("malformed keys accepted: %v", keys)
	}
	if len(ev.fields) != 3 {
		t.Errorf("field count = %d, want 3", len(ev.fields))
	}
}

// TestWALSealingAfterRelease is the amendment-20 contract plus the
// recycle-ABA guarantee: writes through a stale reference stay no-ops
// even after the event has been reset and reused by another request.
func TestWALSealingAfterRelease(t *testing.T) {
	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	ctx := op.Context()
	Add(ctx, "early", "v")

	staleCtx := ctx
	first := op.End(nil)
	if !first {
		t.Fatal("first End should emit")
	}

	// straggler write on the sealed (not yet recycled) event
	Add(staleCtx, "straggler", "x")

	// recycle: another request reuses the pooled event
	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j2"})
	Add(op2.Context(), "other", 1)
	op2.End(nil)

	// the stale reference writes into the recycled event: must no-op
	Add(staleCtx, "corrupt", true)

	events := ts.Events()
	if len(events) != 2 {
		t.Fatalf("captured %d events, want 2", len(events))
	}
	for i, ev := range events {
		if _, bad := ev.Lookup("straggler"); bad {
			t.Errorf("event %d contains straggler write", i)
		}
		if _, bad := ev.Lookup("corrupt"); bad {
			t.Errorf("event %d contains post-recycle write", i)
		}
	}
	last := events[1]
	if _, ok := last.Lookup("other"); !ok {
		t.Error("second request lost its own field")
	}
}

// TestWALArmingProtocol exercises the amendment-1 protocol: armed
// events serialize appends and snapshots under the mutex, and the race
// detector must stay clean (spec scenario: watchdog snapshot mid-flight).
func TestWALArmingProtocol(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	ev.arm()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { // watchdog-style snapshots
		defer wg.Done()
		for range 2000 {
			_ = ev.snapshotFields()
		}
	}()
	go func() { // guarded appends
		defer wg.Done()
		for i := range 2000 {
			ev.append(ref.gen, fieldOf("guarded", i))
		}
	}()
	go func() { // stragglers with stale generations
		defer wg.Done()
		for i := range 2000 {
			ev.append(ref.gen+7, fieldOf("stale", i))
		}
	}()
	wg.Wait()

	for _, f := range ev.fields {
		if f.key == "stale" {
			t.Fatal("stale-generation write accepted")
		}
	}
}

func TestWALLookupLastWriteWins(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	ev.addKV(ref, "k", 1)
	ev.addKV(ref, "k", 2)
	if v, ok := ev.lookup("k"); !ok || v.(int64) != 2 {
		t.Fatalf("lookup = %v %v, want 2", v, ok)
	}
	if _, ok := ev.lookup("missing"); ok {
		t.Fatal("missing key found")
	}
}

func TestWALSetters(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	ev.setError(ref, errors.New("boom"))
	if !ev.hasErr {
		t.Error("hasErr not set")
	}
	if v, ok := ev.lookup("error"); !ok {
		t.Fatal("error field missing")
	} else if m, ok := v.(map[string]any); !ok || m["message"] != "boom" {
		t.Fatalf("error field = %v", v)
	}

	ev.setMessage(ref, "custom")
	if ev.msg != "custom" {
		t.Error("message not set")
	}
	ev.setMessage(ref, "") // empty is unset
	if ev.msg != "custom" {
		t.Error("empty message cleared a set message")
	}

	ev.setRoute(ref, "/x/:id")
	if v, ok := ev.lookup("http.route"); !ok || v.(string) != "/x/:id" {
		t.Fatalf("route = %v", v)
	}
	ev.setRoute(ref, "")
	if v, ok := ev.lookup("http.route"); ok && v.(string) == "" {
		t.Error("empty route written")
	}

	ev.setLevel(ref, LevelWarn)
	if !ev.hasRequestedLvl || ev.requestedLevel != LevelWarn {
		t.Error("level not set")
	}
	ev.setLevel(ref, Level(99))
	if ev.requestedLevel != LevelWarn {
		t.Error("invalid level accepted")
	}
}

func TestWALStartedAt(t *testing.T) {
	before := time.Now()
	ev := newEvent()
	if ev.startedAt.Before(before.Add(-time.Second)) || ev.startedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("startedAt = %v, outside window", ev.startedAt)
	}
}

// TestWALAppendSealedStaleGeneration pins the appendSealed generation
// check — the owner's post-seal write path must reject a stale
// generation exactly like append does. The state word alone is not
// enough: a post-seal write against an event a newer request already
// reset belongs to that request. Found as a gap by mutation testing
// (M3): removing the check passed the whole suite because no test
// drove appendSealed with a stale generation.
//
// Recycle is simulated in place with seal()+reset() — exactly the
// release-then-newEvent sequence — rather than through the pool: the
// race runtime drops a share of sync.Pool Puts (zap internal/pool
// note; TestPoolReuseCanary retries around it), which would make a
// pool round-trip nondeterministic here. reset() is the operation
// that must invalidate the old generation.
func TestWALAppendSealedStaleGeneration(t *testing.T) {
	ev := newEvent()
	staleGen := ev.state.Load() >> walStateBits
	ev.append(staleGen, fieldStr("own", "first"))
	ev.seal()  // End's terminal step (release() seals before pooling)
	ev.reset() // the next request's newEvent() after the pool hands it out

	currentGen := ev.state.Load() >> walStateBits
	if currentGen == staleGen {
		t.Fatal("reset did not advance the generation")
	}

	// The stale owner-style post-seal write must no-op: the event now
	// belongs to another request.
	ev.appendSealed(staleGen, fieldStr("stale", "x"))
	for _, f := range ev.fields {
		if f.key == "stale" {
			t.Fatalf("appendSealed with stale generation %d landed on the recycled event", staleGen)
		}
	}

	// Positive controls: the current generation writes through the same
	// path both before sealing and after it (the real annotatePostSeal
	// shape runs post-seal under the sealed state).
	ev.appendSealed(currentGen, fieldStr("live", "1"))
	ev.seal()
	ev.appendSealed(currentGen, fieldStr("sealed", "2"))
	keys := map[string]bool{}
	for _, f := range ev.fields {
		keys[f.key] = true
	}
	for _, want := range []string{"live", "sealed"} {
		if !keys[want] {
			t.Fatalf("current-generation appendSealed lost %q", want)
		}
	}
	if keys["stale"] {
		t.Fatal("stale write landed next to the current-generation fields")
	}
}

// TestWALStaleSettersAfterReset pins the live() generation checks in
// the setter entry points that do not route through append's gen check:
// setMessage, setLevel, and setError's hasErr latch all trust live()
// alone. Found as a gap by review (GLM finding 1): removing live() from
// setMessage passed the entire non-race suite — the pool canary's
// sentinel oracle only observes field-key landings (the "!BUG" Add),
// never msg/requestedLevel mutations, which are invisible on the wire
// until a later request reuses the event.
// (Black-box counterpart: TestStragglerSettersAfterRecycle drives the
// same invariant through the public API and asserts the emitted
// event; this white-box test pins the internal latches.)
func TestWALStaleSettersAfterReset(t *testing.T) {
	ev := newEvent()
	gen := ev.state.Load() >> walStateBits
	owner := &walRef{ev: ev, gen: gen}
	stale := &walRef{ev: ev, gen: gen}

	// Positive control before the recycle: the owner's setter writes
	// land and latch their flags.
	ev.setMessage(owner, "own")
	ev.setLevel(owner, LevelWarn)
	ev.setError(owner, errors.New("own error"))
	if ev.msg != "own" {
		t.Fatalf("owner setMessage lost: msg=%q", ev.msg)
	}
	if !ev.hasRequestedLvl || ev.requestedLevel != LevelWarn {
		t.Fatalf("owner setLevel lost: level=%v has=%v", ev.requestedLevel, ev.hasRequestedLvl)
	}
	if !ev.hasErr {
		t.Fatal("owner setError lost")
	}

	ev.seal()
	ev.reset() // the next request's newEvent() after the pool hands it out
	if newGen := ev.state.Load() >> walStateBits; newGen == gen {
		t.Fatal("reset did not advance the generation")
	}

	// Stale-generation setter writes must no-op on the reset event — in
	// particular they must not set msg/requestedLevel/hasErr, which the
	// field-key oracle of TestPoolReuseCanary cannot observe.
	ev.setMessage(stale, "!BUG stale message")
	ev.setLevel(stale, LevelError)
	ev.setError(stale, errors.New("!BUG stale error"))
	if ev.msg != "" {
		t.Fatalf("stale setMessage landed on the reset event: msg=%q", ev.msg)
	}
	if ev.hasRequestedLvl || ev.requestedLevel != 0 {
		t.Fatalf("stale setLevel landed on the reset event: level=%v has=%v", ev.requestedLevel, ev.hasRequestedLvl)
	}
	if ev.hasErr {
		t.Fatal("stale setError latched hasErr on the reset event")
	}
	for _, f := range ev.fields {
		if f.key == "error" {
			t.Fatal("stale setError appended an error field on the reset event")
		}
	}

	// Positive control after the recycle: the new owner's setters land.
	freshGen := ev.state.Load() >> walStateBits
	fresh := &walRef{ev: ev, gen: freshGen}
	ev.setMessage(fresh, "new-owner")
	ev.setLevel(fresh, LevelDebug)
	if ev.msg != "new-owner" {
		t.Fatalf("fresh setMessage lost: msg=%q", ev.msg)
	}
	if !ev.hasRequestedLvl || ev.requestedLevel != LevelDebug {
		t.Fatalf("fresh setLevel lost: level=%v has=%v", ev.requestedLevel, ev.hasRequestedLvl)
	}
}

// TestStragglerCannotRaceSinkRead pins the F1 fix (seal before commit):
// writes attempted from inside a sink's Write land on a sealed WAL and
// must be no-ops — the record handed to sinks is immutable.
func TestStragglerCannotRaceSinkRead(t *testing.T) {
	var captured *Record
	var midWriteAccepted atomic.Int32
	rt := MustCompile(Config{
		Sink: sinkFunc(func(ctx context.Context, rec *Record) {
			Add(ctx, "midwrite", 1) // straggler-style write during Write
			Error(ctx, errors.New("midwrite-error"))
			SetMessage(ctx, "midwrite-message")
			SetLevel(ctx, LevelError)
			captured = rec
			if _, ok := rec.Lookup("midwrite"); ok {
				midWriteAccepted.Add(1)
			}
		}),
		SamplingRate: 1,
	})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "real", 1)
	op.End(nil)

	if midWriteAccepted.Load() != 0 {
		t.Fatal("mid-write Add landed on a sealed record")
	}
	if _, ok := captured.Lookup("error"); ok {
		t.Fatal("mid-write Error landed on a sealed record")
	}
	if captured.Message() == "midwrite-message" {
		t.Fatal("mid-write SetMessage mutated a sealed record")
	}
	if captured.Level() == LevelError {
		t.Fatal("mid-write SetLevel mutated a sealed record")
	}
	if v, _ := captured.Lookup("op.outcome"); v != "success" {
		t.Fatalf("outcome mutated: %v", v)
	}
}

// TestStragglerSettersAfterRecycle pins the setter generation checks:
// SetMessage/SetLevel/Error through a stale context must not corrupt
// the next request that reuses the pooled event.
// (White-box counterpart: TestWALStaleSettersAfterReset pins the same
// invariant at the internal-latch level; this test drives the public
// API and asserts the emitted events.)
func TestStragglerSettersAfterRecycle(t *testing.T) {
	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: 1})

	op1 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "one"})
	stale := op1.Context()
	op1.End(nil)

	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "two"})
	op2.End(nil)

	// stale-context setter writes after the pool recycled the event
	SetMessage(stale, "corrupted")
	SetLevel(stale, LevelError)
	Error(stale, errors.New("corrupted"))

	events := ts.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	second := events[1]
	if second.Message() != "operation_completed" {
		t.Fatalf("second message corrupted: %q", second.Message())
	}
	if second.Level() != LevelInfo {
		t.Fatalf("second level corrupted: %v", second.Level())
	}
	if _, ok := second.Lookup("error"); ok {
		t.Fatal("second event corrupted with a stale error")
	}
}

// TestSealDuringArmedAppend pins the armed-path seal protocol: an armed
// append in flight while End seals either lands before the seal or is
// dropped — never a torn or recycled-buffer write.
func TestSealDuringArmedAppend(t *testing.T) {
	rt := MustCompile(Config{Sink: NewTestSink(), SamplingRate: 1})
	var wg sync.WaitGroup
	for range 200 {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "r"})
		ctx := op.Context()
		op.ev.arm() // arm BEFORE End to force the guarded path
		wg.Go(func() {
			for i := range 50 {
				Add(ctx, "armed", i)
			}
		})
		op.End(nil)
		wg.Wait()
	}
}

// TestArmingStaleGeneration pins both recycle directions: stale (past)
// and future generations are rejected.
func TestArmingStaleGeneration(t *testing.T) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	ev.arm()
	ev.append(ref.gen-1, fieldStr("past", "x"))   // stale past
	ev.append(ref.gen+1, fieldStr("future", "x")) // future
	for _, f := range ev.fields {
		if f.key == "past" || f.key == "future" {
			t.Fatal("generation check failed")
		}
	}
}

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
		for i := range rounds {
			op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "churn"})
			Add(op.Context(), "owner", i)
			op.End(nil)
		}
	}()

	// Stragglers: replay the full stale-write vocabulary against the
	// recycled event, released at the same instant as the owner.
	for s := range stragglers {
		go func(s int) {
			defer wg.Done()
			enter()
			for i := range rounds {
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
		for s := range stragglers {
			if _, ok := ev.Lookup(fmt.Sprintf("!BUG-straggler-%d", s)); ok {
				t.Fatalf("event %d contains a straggler write — stale context "+
					"mutated a sealed/recycled event (amendment 20)", i)
			}
		}
	}
}

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
	// request's — may ever carry the sentinel, whether as a field key
	// (the Add shape) or as a msg/level mutation (SetMessage/SetLevel
	// write no field — without these checks those landings would be
	// invisible to a key scan; review finding GLM-1). The assertion
	// message names the contract so a regression report points at the
	// fix, in slog's "!BUG" spirit.
	for i, ev := range ts.Events() {
		if v, ok := ev.Lookup("!BUG"); ok {
			t.Fatalf("event %d: use-after-recycle — a write through a stale "+
				"context landed on a pooled event (slog !BUG canary): %v; "+
				"the generation check in append/setMessage/setLevel "+
				"(wal.go amendments 1/20) must reject post-End writes", i, v)
		}
		if msg := ev.Message(); msg == "!BUG stale message" {
			t.Fatalf("event %d: stale SetMessage landed on a pooled event "+
				"(message %q); the live() generation check in setMessage must "+
				"reject post-End writes", i, msg)
		}
		if ev.Level() == LevelError {
			t.Fatalf("event %d: stale SetLevel(Error) landed on a pooled event; "+
				"the live() generation check in setLevel must reject post-End writes", i)
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

// Loom-lite oracle note.
//
// The one preemption-sensitive fragment is the straggler's append: its
// single state load happens, then it acts. Between load and act the
// owner may seal or recycle, so the straggler steps are split
// (load / act) exactly at that boundary; the act replays the real
// post-load fragment (live: direct append — the documented residual;
// armed: lock + recheck + append).
//
// Invariants checked at the end of every schedule:
//
//  1. A straggler whose load observed a sealed state or a mismatched
//     generation never lands; live-load landings may land late (the
//     documented nanosecond-scale residual) but always exactly once;
//     armed-load landings depend on the in-lock recheck.
//  2. The owner's post-seal writes (appendSealed) always land.
//  3. Snapshots are never torn: each equals the field prefix at its
//     copy point.
//  4. Recycle: a stale-generation write never lands when its load
//     observed the new generation.
//  5. The mutex is never held by two actors and every schedule is
//     deadlock-free.
//  6. Linearizability: the final fields (LWW-folded) equal an
//     independent ordered reference map that applies every successful
//     append at its landing point.

// Shadow event

// simEvent mirrors the real event's observable protocol state: the
// generation + state word, the mutex holder, the append-only fields,
// and the setter state (message). All values are plain — the schedule
// is the only source of ordering.
type simEvent struct {
	gen    uint64
	state  walState // real constants: walLive/walArmed/walSealed/walSealedArmed
	muHeld bool

	fields []string // keys in landing order (reset clears)
	msg    string
}

// stragglerMode is the decision a load step records for the later act.
type stragglerMode uint8

const (
	modeDrop     stragglerMode = iota // load saw sealed or a mismatched generation
	modeLive                          // load saw live: unconditional landing at act
	modeArmed                         // load saw armed: mu + recheck at act
	modeSetLive                       // setter load saw live
	modeSetArmed                      // setter load saw armed (mu + recheck at act)
	modeSetDrop                       // setter load saw sealed / wrong gen
)

// stragglerPlan is the per-straggler decision state, shared by its
// load and act steps (mirrors the real goroutine's load-then-act).
type stragglerPlan struct {
	refGen    uint64 // the generation the straggler believes it holds
	mode      stragglerMode
	loadState walState
	loadGen   uint64
	actState  walState // state observed at act time (for the re-derivation)
	actGen    uint64
	landed    bool // act outcome (for the invariant checks)
	key       string
	msg       string
}

// simSnapshot is one snapshotter copy: the field keys plus the landing
// sequence number at copy time.
type simSnapshot struct {
	keys []string
	seq  int
}

// sim is the per-schedule simulation state. It is copied at every
// branch of the enumerator (small: at most a dozen fields).
type sim struct {
	ev        simEvent
	plans     []stragglerPlan
	landings  []string // every successful append, in order (reset clears)
	seq       int      // global landing counter (never resets)
	snapshots []simSnapshot
	log       []string // schedule log for failure messages
	flags     map[string]bool

	// stepsRun is the executed schedule (for the real-event replay);
	// postKeys lists the owner's post-seal writes for invariant 2;
	// realSnapshots collects the real event's snapshot copies during
	// replay.
	stepsRun      []*simStep
	postKeys      []string
	realSnapshots [][]string
}

func newSim(gen uint64, nStragglers int) *sim {
	return &sim{
		ev:    simEvent{gen: gen, state: walLive},
		plans: make([]stragglerPlan, nStragglers),
		flags: map[string]bool{},
	}
}

func (s *sim) clone() *sim {
	c := *s
	c.ev.fields = append([]string(nil), s.ev.fields...)
	c.landings = append([]string(nil), s.landings...)
	c.log = append([]string(nil), s.log...)
	c.snapshots = make([]simSnapshot, len(s.snapshots))
	for i, sn := range s.snapshots {
		c.snapshots[i] = simSnapshot{keys: append([]string(nil), sn.keys...), seq: sn.seq}
	}
	c.plans = append([]stragglerPlan(nil), s.plans...)
	c.stepsRun = append([]*simStep(nil), s.stepsRun...)
	c.realSnapshots = append([][]string(nil), s.realSnapshots...)
	c.postKeys = append([]string(nil), s.postKeys...)
	c.flags = map[string]bool{}
	for k, v := range s.flags {
		c.flags[k] = v
	}
	return &c
}

// Shadow protocol operations (each mirrors one wal.go function; the
// mirror mapping is annotated so a production change to the real
// protocol must update the mirror or the replay comparison fails).

// simAppend mirrors event.append's live fast path and guarded path as
// one schedule step (the owner's own adds are not split — the real
// owner is the single writer of its generation).
func (s *sim) simAppend(key string) {
	switch s.ev.state {
	case walLive:
		s.land(key)
	case walArmed:
		// guarded: the caller checked muHeld (runnable gate)
		s.land(key)
	}
}

// simAppendSealed mirrors event.appendSealed (owner post-seal writes):
// the generation always matches (the owner runs it), so it lands on
// live/sealed and on sealedArmed under the mutex.
func (s *sim) simAppendSealed(key string) {
	s.land(key)
}

// simSeal mirrors event.seal.
func (s *sim) simSeal() {
	switch s.ev.state {
	case walArmed:
		s.releaseMu()
		s.ev.state = walSealedArmed
	case walLive:
		s.ev.state = walSealed
	}
	// sealed states: idempotent
}

// simArm mirrors event.arm: live → armed under the mutex.
func (s *sim) simArm() {
	if s.ev.state == walLive {
		s.acquireMu()
		s.ev.state = walArmed
		s.releaseMu()
	}
}

// simReset mirrors event.reset: the next generation, live, cleared.
func (s *sim) simReset() {
	s.ev.gen++
	s.ev.state = walLive
	s.ev.fields = nil
	s.ev.msg = ""
	s.landings = nil
}

func (s *sim) land(key string) {
	s.ev.fields = append(s.ev.fields, key)
	s.landings = append(s.landings, key)
	s.seq++
}

func (s *sim) acquireMu() { s.ev.muHeld = true }
func (s *sim) releaseMu() { s.ev.muHeld = false }

// stragglerLoad mirrors the single state load at the top of
// event.append: it records the decision the act step will execute.
func (s *sim) stragglerLoad(plan int) {
	p := &s.plans[plan]
	p.loadGen = s.ev.gen
	p.loadState = s.ev.state
	switch {
	case p.loadGen != p.refGen:
		p.mode = modeDrop
	case p.loadState == walLive:
		p.mode = modeLive
	case p.loadState == walArmed:
		p.mode = modeArmed
	default:
		p.mode = modeDrop
	}
}

// stragglerActRunnable reports whether the act step may run: an armed
// decision needs the mutex, which a concurrent holder blocks.
func (s *sim) stragglerActRunnable(plan int) bool {
	return s.plans[plan].mode != modeArmed || !s.ev.muHeld
}

// stragglerAct mirrors the post-load fragment of event.append: live
// decisions append unconditionally (the documented residual — the load
// may predate a seal or recycle), armed decisions lock, recheck the
// state word, and append only while still armed.
func (s *sim) stragglerAct(plan int) {
	p := &s.plans[plan]
	p.actGen = s.ev.gen
	p.actState = s.ev.state
	switch p.mode {
	case modeDrop:
	case modeLive:
		s.land(p.key)
		p.landed = true
	case modeArmed:
		s.acquireMu()
		if s.ev.gen == p.refGen && s.ev.state == walArmed {
			s.land(p.key)
			p.landed = true
		}
		s.releaseMu()
	}
}

// setterLoad/Act mirror event.setMessage: live writes the message,
// armed writes under the mutex with a recheck, sealed states drop.
func (s *sim) setterLoad(plan int) {
	p := &s.plans[plan]
	p.loadGen = s.ev.gen
	p.loadState = s.ev.state
	switch {
	case p.loadGen != p.refGen:
		p.mode = modeSetDrop
	case p.loadState == walLive:
		p.mode = modeSetLive
	case p.loadState == walArmed:
		p.mode = modeSetArmed
	default:
		p.mode = modeSetDrop
	}
}

func (s *sim) setterActRunnable(plan int) bool {
	return s.plans[plan].mode != modeSetArmed || !s.ev.muHeld
}

func (s *sim) setterAct(plan int) {
	p := &s.plans[plan]
	p.actGen = s.ev.gen
	p.actState = s.ev.state
	switch p.mode {
	case modeSetLive:
		s.ev.msg = p.msg
		p.landed = true
	case modeSetArmed:
		s.acquireMu()
		if s.ev.gen == p.refGen && s.ev.state == walArmed {
			s.ev.msg = p.msg
			p.landed = true
		}
		s.releaseMu()
	}
}

// Scheduler

// simStep is one preemption-free protocol step. runnable gates the
// step on protocol preconditions (mutex ownership, cross-actor order
// such as release-before-recycle); run mutates the sim. real is the
// equivalent fragment against the REAL event, executed during the
// schedule replay (nil means the step has no real effect — e.g. a load
// that only observes).
type simStep struct {
	name     string
	runnable func(s *sim) bool
	run      func(s *sim)
	real     func(ev *event, s *sim)
}

type simActor struct {
	name  string
	steps []simStep
}

// scheduleResult summarizes one enumeration run.
type scheduleResult struct {
	completed int
	deadlocks []string
}

// enumerateSchedules walks every interleaving of the actor step lists
// (the actors' internal order is fixed; the schedule picks the next
// runnable step from any actor). verify runs at every completed
// schedule; replayAndCompare additionally replays the schedule against
// a real event and compares end states (pass real = true).
func enumerateSchedules(t *testing.T, actors []simActor, base *sim, verify func(t *testing.T, s *sim)) scheduleResult {
	t.Helper()
	res := scheduleResult{}
	pos := make([]int, len(actors))

	var rec func(cur *sim)
	rec = func(cur *sim) {
		done := true
		for i := range actors {
			if pos[i] < len(actors[i].steps) {
				done = false
				break
			}
		}
		if done {
			res.completed++
			verify(t, cur)
			return
		}
		advanced := false
		for i := range actors {
			if pos[i] >= len(actors[i].steps) {
				continue
			}
			st := actors[i].steps[pos[i]]
			if st.runnable != nil && !st.runnable(cur) {
				continue
			}
			child := cur.clone()
			child.log = append(child.log, actors[i].name+":"+st.name)
			pos[i]++
			child.stepsRun = append(child.stepsRun, &actors[i].steps[pos[i]-1])
			st.run(child)
			rec(child)
			pos[i]--
			advanced = true
		}
		if !advanced && !done {
			// Remaining steps exist but none is runnable: the schedule
			// deadlocked. A faithful protocol never deadlocks — the
			// mutex holder always has a runnable unlock/act.
			res.deadlocks = append(res.deadlocks, strings.Join(cur.log, " | "))
		}
	}
	rec(base)
	return res
}

// reportScheduleResults fails the test on deadlocks and logs the
// completed count.
func reportScheduleResults(t *testing.T, res scheduleResult, want int) {
	t.Helper()
	for _, d := range res.deadlocks {
		t.Errorf("deadlocked schedule: %s", d)
	}
	t.Logf("enumerated %d schedules (want %d)", res.completed, want)
	if want >= 0 && res.completed != want {
		t.Errorf("enumerated %d schedules, want %d", res.completed, want)
	}
}

// Real-event replay

// replayAndCompare runs the executed schedule against a REAL event
// using the real protocol functions and the real post-load fragments,
// then compares the end state with the shadow's. The event is a fresh
// zero value + reset(): a deterministic base generation of 1, immune
// to the pool's recycled-generation drift (newEvent would inherit
// whatever generation earlier tests left in the pool).
func replayAndCompare(t *testing.T, s *sim) {
	t.Helper()
	ev := &event{}
	ev.reset()

	for _, st := range s.stepsRun {
		if st.real != nil {
			st.real(ev, s)
		}
	}

	// Fields must match key-for-key (the shadow's landing order is the
	// real append order under the same schedule).
	if got, want := fieldKeys(ev.fields), s.ev.fields; !equalStrings(got, want) {
		t.Fatalf("real fields %v != shadow fields %v\nschedule: %s", got, want, scheduleLog(s))
	}
	// State word: same state and same generation count.
	realState := walState(ev.state.Load() & walStateMask)
	if realState != s.ev.state {
		t.Fatalf("real state %v != shadow state %v\nschedule: %s", realState, s.ev.state, scheduleLog(s))
	}
	if got := ev.state.Load() >> walStateBits; got != s.ev.gen {
		t.Fatalf("real gen %d != shadow gen %d\nschedule: %s", got, s.ev.gen, scheduleLog(s))
	}
	if ev.msg != s.ev.msg {
		t.Fatalf("real msg %q != shadow %q\nschedule: %s", ev.msg, s.ev.msg, scheduleLog(s))
	}
	// Snapshots must match the shadow's copies.
	if len(s.realSnapshots) != len(s.snapshots) {
		t.Fatalf("real snapshots %d != shadow %d\nschedule: %s",
			len(s.realSnapshots), len(s.snapshots), scheduleLog(s))
	}
	for i := range s.snapshots {
		if got := s.realSnapshots[i]; !equalStrings(got, s.snapshots[i].keys) {
			t.Fatalf("snapshot %d: real %v != shadow %v\nschedule: %s",
				i, got, s.snapshots[i].keys, scheduleLog(s))
		}
	}
}

func fieldKeys(fields []Field) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.key
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func scheduleLog(s *sim) string { return strings.Join(s.log, " | ") }

// Shared schedule verification (invariants + linearizability)

// verifySchedule runs the invariant checks for one completed schedule:
// landing legality re-derived from the recorded state history, owner
// post-seal writes present, snapshot prefix consistency, and the
// reference-map linearizability check. replay selects whether the
// schedule is also replayed against the real event.
func verifySchedule(t *testing.T, s *sim, replay bool) {
	t.Helper()
	if replay {
		replayAndCompare(t, s)
	}

	// Invariant 1 + the reference re-derivation: every straggler
	// attempt's outcome is recomputed from the recorded load/act state
	// history and must match what actually happened.
	for i := range s.plans {
		p := &s.plans[i]
		want := false
		switch p.mode {
		case modeLive, modeSetLive:
			// A live load with a matching generation lands
			// unconditionally at act — even across a seal or recycle
			// (the documented residual).
			want = p.loadGen == p.refGen
		case modeArmed, modeSetArmed:
			// An armed load lands iff the act-time recheck still sees
			// the same generation in the armed state.
			want = p.actGen == p.refGen && p.actState == walArmed
		}
		if p.landed != want {
			t.Fatalf("straggler %d: landed=%v, re-derived=%v (load gen %d state %v, act gen %d state %v)\nschedule: %s",
				i, p.landed, want, p.loadGen, p.loadState, p.actGen, p.actState, scheduleLog(s))
		}
		if !p.landed {
			// A dropped attempt must not appear on the wire.
			if key := p.key; key != "" && slices.Contains(s.ev.fields, key) {
				t.Fatalf("straggler %d key %q landed despite a drop decision\nschedule: %s",
					i, key, scheduleLog(s))
			}
		}
	}

	// Invariant 2: the owner's post-seal writes always land.
	for _, k := range s.postKeys {
		if !slices.Contains(s.ev.fields, k) {
			t.Fatalf("owner post-seal write %q missing from %v\nschedule: %s",
				k, s.ev.fields, scheduleLog(s))
		}
	}

	// Invariant 3: snapshots are prefix-consistent (no torn state).
	for i, sn := range s.snapshots {
		if len(s.ev.fields) < len(sn.keys) {
			t.Fatalf("snapshot %d longer than the fields\nschedule: %s", i, scheduleLog(s))
		}
		if !equalStrings(s.ev.fields[:len(sn.keys)], sn.keys) {
			t.Fatalf("snapshot %d %v is not a prefix of %v\nschedule: %s",
				i, sn.keys, s.ev.fields, scheduleLog(s))
		}
	}

	// Linearizability: fold the fields (last write per key at its last
	// occurrence) and compare against an independent ordered map that
	// applies every landed append as an overwrite at its landing point.
	// The reference map applies every landing as an overwrite; its
	// final key set and last-write order must equal the LWW fold of
	// the fields (landings == fields by construction, so this checks
	// the bookkeeping: any append that mutated fields without logging
	// a landing — or vice versa — breaks the equality).
	gotFold := foldKeys(s.ev.fields)
	mapKeys := foldKeys(s.landings)
	if !equalStrings(gotFold, mapKeys) {
		t.Fatalf("linearizability: LWW fold %v != reference map %v\nschedule: %s",
			gotFold, mapKeys, scheduleLog(s))
	}
}

// foldKeys resolves a key list last-write-wins: each key once, its
// last occurrence, in last-occurrence order (the encoder's contract).
func foldKeys(keys []string) []string {
	last := map[string]int{}
	for i, k := range keys {
		last[k] = i
	}
	order := make([]int, 0, len(last))
	for _, i := range last {
		order = append(order, i)
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && order[j] < order[j-1]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	out := make([]string, 0, len(order))
	for _, i := range order {
		out = append(out, keys[i])
	}
	return out
}

// Scenario builders and helpers

// genOf reads the current generation of a real event (gen units).
func genOf(ev *event) uint64 { return ev.state.Load() >> walStateBits }

func addOp(key string) simStep {
	run := func(s *sim) { s.simAppend(key) }
	real := func(ev *event, _ *sim) { ev.append(genOf(ev), fieldOf(key, key)) }
	return simStep{
		name:     "add-" + key,
		runnable: func(s *sim) bool { return s.ev.state != walArmed || !s.ev.muHeld },
		run:      run,
		real:     real,
	}
}

func postOp(key string) simStep {
	return simStep{
		name: "post-" + key,
		run:  func(s *sim) { s.simAppendSealed(key) },
		real: func(ev *event, _ *sim) { ev.appendSealed(genOf(ev), fieldOf(key, key)) },
	}
}

func sealOp() simStep {
	return simStep{
		name:     "seal",
		runnable: func(s *sim) bool { return s.ev.state != walArmed || !s.ev.muHeld },
		run:      func(s *sim) { s.simSeal() },
		real:     func(ev *event, _ *sim) { ev.seal() },
	}
}

func armOp() simStep {
	return simStep{
		name:     "arm",
		runnable: func(s *sim) bool { return !s.ev.muHeld },
		run:      func(s *sim) { s.simArm() },
		real:     func(ev *event, _ *sim) { ev.arm() },
	}
}

func releaseOp(flag string) simStep {
	return simStep{
		name: "release",
		run: func(s *sim) {
			s.simSeal()
			s.flags[flag] = true
		},
		real: func(ev *event, _ *sim) { ev.seal() }, // pool handoff omitted: the gen is the protection
	}
}

func resetOp(flag string) simStep {
	return simStep{
		name: "reset",
		// The pool hands the event to request 2 only after request 1
		// released it.
		runnable: func(s *sim) bool { return s.flags[flag] },
		run:      func(s *sim) { s.simReset() },
		real:     func(ev *event, _ *sim) { ev.reset() },
	}
}

// stragglerSteps returns the load/act step pair for one append
// straggler. The load mirrors the real append's single state load; the
// act mirrors the post-load fragment (live: unconditional append —
// the documented residual; armed: lock + recheck + append).
func stragglerSteps(name string, plan int, refGen uint64, key string) []simStep {
	return []simStep{
		{name: name + "-load", run: func(s *sim) { s.stragglerLoad(plan) }},
		{
			name:     name + "-act",
			runnable: func(s *sim) bool { return s.stragglerActRunnable(plan) },
			run:      func(s *sim) { s.stragglerAct(plan) },
			real: func(ev *event, s *sim) {
				p := &s.plans[plan]
				switch p.mode {
				case modeLive:
					// The documented residual: the load predated the
					// seal/recycle; the append lands regardless.
					ev.fields = append(ev.fields, fieldOf(key, key))
				case modeArmed:
					ev.mu.Lock()
					if cur := ev.state.Load(); cur>>walStateBits == p.refGen &&
						walState(cur&walStateMask) == walArmed {
						ev.fields = append(ev.fields, fieldOf(key, key))
					}
					ev.mu.Unlock()
				}
			},
		},
	}
}

// setterSteps returns the load/act pair for one SetMessage straggler
// (the armed-mu discipline pinned in T3).
func setterSteps(name string, plan int, refGen uint64, msg string) []simStep {
	return []simStep{
		{name: name + "-load", run: func(s *sim) { s.setterLoad(plan) }},
		{
			name:     name + "-act",
			runnable: func(s *sim) bool { return s.setterActRunnable(plan) },
			run:      func(s *sim) { s.setterAct(plan) },
			real: func(ev *event, s *sim) {
				p := &s.plans[plan]
				switch p.mode {
				case modeSetLive:
					ev.msg = msg
				case modeSetArmed:
					ev.mu.Lock()
					if cur := ev.state.Load(); cur>>walStateBits == p.refGen &&
						walState(cur&walStateMask) == walArmed {
						ev.msg = msg
					}
					ev.mu.Unlock()
				}
			},
		},
	}
}

// snapshotSteps returns the lock/copy/unlock triple. Snapshots are the
// armed watchdog read (the protocol's usage contract), so the lock is
// only runnable while the event is armed and the mutex is free.
func snapshotSteps() []simStep {
	lock := simStep{
		name: "snap-lock",
		// The watchdog snapshots armed events; once armed, a snapshot
		// may be taken before or after the seal (sealedArmed) whenever
		// the mutex is free — the real snapshotFields has no state
		// check. Live-state snapshots are excluded by the usage
		// contract (they would race the owner's lock-free fast path).
		runnable: func(s *sim) bool {
			return (s.ev.state == walArmed || s.ev.state == walSealedArmed) && !s.ev.muHeld
		},
		run: func(s *sim) { s.acquireMu() },
	}
	copy := simStep{
		name: "snap-copy",
		run: func(s *sim) {
			s.snapshots = append(s.snapshots, simSnapshot{
				keys: append([]string(nil), s.ev.fields...),
				seq:  s.seq,
			})
		},
		real: func(ev *event, s *sim) {
			s.realSnapshots = append(s.realSnapshots, fieldKeys(ev.snapshotFields()))
		},
	}
	unlock := simStep{name: "snap-unlock", run: func(s *sim) { s.releaseMu() }}
	return []simStep{lock, copy, unlock}
}

// runScenario enumerates one scenario's schedules and verifies each.
// want pins the completed-schedule count: the enumeration is fully
// deterministic, so a count drift means the scenario, the shadow, or
// the runnable gates changed.
func runScenario(t *testing.T, label string, actors []simActor, base *sim, replay bool, want int) {
	t.Helper()
	res := enumerateSchedules(t, actors, base, func(t *testing.T, s *sim) {
		verifySchedule(t, s, replay)
	})
	reportScheduleResults(t, res, want)
	if res.completed == 0 {
		t.Fatalf("%s: no schedules enumerated", label)
	}
}

// Scenario tests

// TestLoomLiteSealRace: unarmed owner sealing while two current-gen
// stragglers are in flight. Every schedule must produce a protocol-
// legal field history; the real event must match the shadow. Raw
// multinomial: 9!/(5!·2!·2!) = 756 schedules.
func TestLoomLiteSealRace(t *testing.T) {
	base := newSim(1, 2)
	base.plans[0] = stragglerPlan{refGen: 1, key: "s1"}
	base.plans[1] = stragglerPlan{refGen: 1, key: "s2"}
	base.postKeys = []string{"o.outcome", "o.code"}

	owner := simActor{name: "owner", steps: []simStep{
		addOp("o1"), addOp("o2"), sealOp(),
		postOp("o.outcome"), postOp("o.code"),
	}}
	s1 := simActor{name: "s1", steps: stragglerSteps("s1", 0, 1, "s1")}
	s2 := simActor{name: "s2", steps: stragglerSteps("s2", 1, 1, "s2")}
	runScenario(t, "seal-race", []simActor{owner, s1, s2}, base, true, 756)
}

// TestLoomLiteArmedSnapshot: armed owner sealing under the mutex while
// a straggler and a snapshotter interleave. The arm-before-snapshot
// usage constraint trims the raw multinomial 10!/(5!·2!·3!) = 2520.
func TestLoomLiteArmedSnapshot(t *testing.T) {
	base := newSim(1, 1)
	base.plans[0] = stragglerPlan{refGen: 1, key: "s1"}
	base.postKeys = []string{"o.outcome"}

	owner := simActor{name: "owner", steps: []simStep{
		armOp(), addOp("o1"), addOp("o2"), sealOp(), postOp("o.outcome"),
	}}
	s1 := simActor{name: "s1", steps: stragglerSteps("s1", 0, 1, "s1")}
	snap := simActor{name: "snap", steps: snapshotSteps()}
	runScenario(t, "armed-snapshot", []simActor{owner, s1, snap}, base, true, 264)
}

// TestLoomLiteRecycleStale: request 1 seals and releases; request 2
// recycles the event (reset bumps the generation); a stale straggler
// holding request 1's generation interleaves everywhere. Its write may
// land only when its LOAD predated request 1's seal (the documented
// residual); a post-recycle load sees the new generation and drops.
// The release-before-reset pool rule trims the raw multinomial
// 9!/(4!·3!·2!) = 1260.
func TestLoomLiteRecycleStale(t *testing.T) {
	base := newSim(1, 1)
	base.plans[0] = stragglerPlan{refGen: 1, key: "s1"}

	owner1 := simActor{name: "req1", steps: []simStep{
		addOp("o1"), sealOp(), postOp("o1.outcome"), releaseOp("released"),
	}}
	stale := simActor{name: "stale", steps: stragglerSteps("stale", 0, 1, "s1")}
	owner2 := simActor{name: "req2", steps: []simStep{
		resetOp("released"), addOp("o2"), sealOp(),
	}}
	runScenario(t, "recycle-stale", []simActor{owner1, stale, owner2}, base, true, 36)
}

// TestLoomLiteArmedTwoStragglers: the largest state space — two
// stragglers and a snapshotter around an armed seal. Raw multinomial:
// 11!/(4!·2!·2!·3!) = 69,300 schedules.
func TestLoomLiteArmedTwoStragglers(t *testing.T) {
	base := newSim(1, 2)
	base.plans[0] = stragglerPlan{refGen: 1, key: "s1"}
	base.plans[1] = stragglerPlan{refGen: 1, key: "s2"}
	base.postKeys = []string{"o.outcome"}

	owner := simActor{name: "owner", steps: []simStep{
		armOp(), addOp("o1"), sealOp(), postOp("o.outcome"),
	}}
	s1 := simActor{name: "s1", steps: stragglerSteps("s1", 0, 1, "s1")}
	s2 := simActor{name: "s2", steps: stragglerSteps("s2", 1, 1, "s2")}
	snap := simActor{name: "snap", steps: snapshotSteps()}
	runScenario(t, "armed-two-stragglers", []simActor{owner, s1, s2, snap}, base, true, 8756)
}

// TestLoomLiteSetterRaces: SetMessage stragglers (unarmed and armed).
// The message may change only when the setter's load saw a live/armed
// state (armed: recheck at act); a post-seal load never writes.
func TestLoomLiteSetterRaces(t *testing.T) {
	t.Run("unarmed", func(t *testing.T) {
		base := newSim(1, 1)
		base.plans[0] = stragglerPlan{refGen: 1, msg: "stale-msg"}
		owner := simActor{name: "owner", steps: []simStep{addOp("o1"), sealOp()}}
		set := simActor{name: "set", steps: setterSteps("set", 0, 1, "stale-msg")}
		runScenario(t, "setter-unarmed", []simActor{owner, set}, base, true, 6)
	})
	t.Run("armed", func(t *testing.T) {
		base := newSim(1, 1)
		base.plans[0] = stragglerPlan{refGen: 1, msg: "stale-msg"}
		owner := simActor{name: "owner", steps: []simStep{armOp(), addOp("o1"), sealOp()}}
		set := simActor{name: "set", steps: setterSteps("set", 0, 1, "stale-msg")}
		runScenario(t, "setter-armed", []simActor{owner, set}, base, true, 10)
	})
}

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
	annotatePostSeal(ev, &op.ref, start, duration, normalizeDomain(start.Domain) == DomainHTTP, scan, outcome)
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
	for range 8 {
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
	for round := range 50 {
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
		for range stragglers {
			wg.Go(func() {
				mu.Lock()
				ready++
				start.Broadcast()
				for !released {
					start.Wait()
				}
				mu.Unlock()
				stragglerWrite(ctx) // armed: serialized under ev.mu
			})
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
	for round := range 40 {
		sink := &matrixSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "setter-race"})
		ctx := op.Context()
		op.ev.arm()

		const setters = 6
		const writes = 2000
		var wg sync.WaitGroup
		for i := range setters {
			wg.Go(func() {
				for range writes {
					SetMessage(ctx, fmt.Sprintf("msg-%d", i))
					SetLevel(ctx, LevelWarn)
				}
			})
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
