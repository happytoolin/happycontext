package hc

// WAL state machine, sealing, straggler, and pool-recycle tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- from wal_test.go ---
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
		for i := 0; i < 2000; i++ {
			_ = ev.snapshotFields()
		}
	}()
	go func() { // guarded appends
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			ev.append(ref.gen, fieldOf("guarded", i))
		}
	}()
	go func() { // stragglers with stale generations
		defer wg.Done()
		for i := 0; i < 2000; i++ {
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
	if !ev.hasMsg || ev.msg != "custom" {
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
	if !ev.hasMsg || ev.msg != "own" {
		t.Fatalf("owner setMessage lost: msg=%q hasMsg=%v", ev.msg, ev.hasMsg)
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
	if ev.hasMsg || ev.msg != "" {
		t.Fatalf("stale setMessage landed on the reset event: msg=%q hasMsg=%v", ev.msg, ev.hasMsg)
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
	if !ev.hasMsg || ev.msg != "new-owner" {
		t.Fatalf("fresh setMessage lost: msg=%q hasMsg=%v", ev.msg, ev.hasMsg)
	}
	if !ev.hasRequestedLvl || ev.requestedLevel != LevelDebug {
		t.Fatalf("fresh setLevel lost: level=%v has=%v", ev.requestedLevel, ev.hasRequestedLvl)
	}
}

// --- from review_fixes_test.go ---
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

// TestDedupeCrossover pins the 24/25-field boundary between the
// allocation-free scan and the slot-array path.
func TestDedupeCrossover(t *testing.T) {
	for _, n := range []int{23, 24, 25, 26, 33, 40} {
		fields := make([]Field, 0, n)
		for i := 0; i < n-1; i++ {
			fields = append(fields, fieldStr(strings.Repeat("k", i+2), "v"))
		}
		fields = append(fields, fieldStr("kk", "last")) // dup of the first key
		r := recOf(LevelInfo, "m", fields...)
		line := string(r.Encoded())
		if strings.Count(line, `"kk":`) != 1 {
			t.Fatalf("n=%d: dup emitted more than once", n)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if payload["kk"] != "last" {
			t.Fatalf("n=%d: last write lost: %v", n, payload["kk"])
		}
	}
}

// TestSealDuringArmedAppend pins the armed-path seal protocol: an armed
// append in flight while End seals either lands before the seal or is
// dropped — never a torn or recycled-buffer write.
func TestSealDuringArmedAppend(t *testing.T) {
	rt := MustCompile(Config{Sink: NewTestSink(), SamplingRate: 1})
	var wg sync.WaitGroup
	for round := 0; round < 200; round++ {
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "r"})
		ctx := op.Context()
		op.ev.arm() // arm BEFORE End to force the guarded path
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				Add(ctx, "armed", i)
			}
		}()
		op.End(nil)
		wg.Wait()
	}
}

// TestFloat32WireParity pins the float32 kind: 0.1 must render as 0.1,
// not the widened double digits the v0 adapter never emitted.
func TestFloat32WireParity(t *testing.T) {
	r := recOf(LevelInfo, "m", fieldOf("f32", float32(0.1)), fieldOf("f64", 0.1))
	line := string(r.Encoded())
	if !strings.Contains(line, `"f32":0.1`) {
		t.Fatalf("float32 widened on the wire: %s", line)
	}
	if !strings.Contains(line, `"f64":0.1`) {
		t.Fatalf("float64 broken: %s", line)
	}
	// round-trip through the typed getter
	f := r.Fields()[0]
	if v, ok := f.Float(); !ok || math.Abs(v-0.1) > 1e-7 { // float64 getter: float32 epsilon
		t.Fatalf("Float() = %v %v", v, ok)
	}
	if _, isF32 := valueOf(f).(float32); !isF32 {
		t.Fatalf("valueOf lost float32-ness: %T", valueOf(f))
	}
}

// TestCompileRuntimeImmutable pins the deep copy: mutating the caller's
// Config (and its nested maps) after Compile cannot affect the runtime.
func TestCompileRuntimeImmutable(t *testing.T) {
	rate := 0.5
	cfg := Config{
		Sink:         NewTestSink(),
		SamplingRate: 1,
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {
				SuccessLevel:  LevelInfo,
				OutcomeLevels: map[Outcome]Level{OutcomeRetry: LevelWarn},
				SamplingRate:  &rate,
			},
		},
		LevelSamplingRates: map[Level]float64{LevelWarn: 1},
	}
	rt, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mutated := cfg.OperationPolicies[DomainJob]
	mutated.SuccessLevel = LevelError
	mutated.OutcomeLevels[OutcomeRetry] = LevelDebug
	*mutated.SamplingRate = 0
	cfg.LevelSamplingRates[LevelWarn] = 0
	cfg.Message = "mutated"

	if p := rt.policyFor(DomainJob); p.SuccessLevel != LevelInfo {
		t.Fatalf("policy level mutated: %v", p.SuccessLevel)
	} else if p.OutcomeLevels[OutcomeRetry] != LevelWarn {
		t.Fatalf("outcome level mutated: %v", p.OutcomeLevels[OutcomeRetry])
	} else if *p.SamplingRate != 0.5 {
		t.Fatalf("sampling rate mutated: %v", *p.SamplingRate)
	}
	if rt.levelRates[LevelWarn] != 1 || rt.message != "" {
		t.Fatal("level rates or message mutated")
	}
}

// TestPolicyAliasPrecedence pins the deterministic winner: an explicit
// "operation" key beats the "" alias when both are present.
func TestPolicyAliasPrecedence(t *testing.T) {
	rt := MustCompile(Config{
		SamplingRate: 1,
		OperationPolicies: map[Domain]OperationPolicy{
			"":          {SuccessLevel: LevelWarn},
			"operation": {SuccessLevel: LevelDebug},
		},
	})
	if p := rt.policyFor(Domain("")); p.SuccessLevel != LevelDebug {
		t.Fatalf("alias won over explicit key: %+v", p)
	}
}

// TestSampleInputFieldsAssertion pins amendment 8 with a real assertion
// (Fields() iteration sees the request's fields during sampling).
func TestSampleInputFieldsAssertion(t *testing.T) {
	var fieldCount, sawCounter int
	rt := MustCompile(Config{
		Sink:         NewTestSink(),
		SamplingRate: 1,
		Sampler: func(in SampleInput) bool {
			fieldCount = len(in.Fields())
			for _, f := range in.Fields() {
				if f.Key() == "counter" {
					if v, ok := f.Int(); ok {
						sawCounter = int(v)
					}
				}
			}
			return true
		},
	})
	op := Start(context.Background(), rt, OperationStart{})
	Add(op.Context(), "counter", 41, "other", "x")
	op.End(nil)
	if sawCounter != 41 || fieldCount < 2 {
		t.Fatalf("Fields() view: counter=%d fields=%d", sawCounter, fieldCount)
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

// --- from straggler_start_line_test.go ---
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

// --- from pool_reuse_canary_test.go ---
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
