package hc

import (
	"context"
	"errors"
	"sync"
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
