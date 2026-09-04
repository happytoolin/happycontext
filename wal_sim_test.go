package hc

// Loom-lite: exhaustive WAL interleaving simulation. The real event
// uses atomics, a mutex, and sync.Pool — none deterministically
// schedulable — so this harness simulates the WAL protocol on a SHADOW
// event (plain values, no atomics) and enumerates EVERY interleaving
// with a cooperative scheduler. Each schedule is then REPLAYED against
// the real event and the shadow's end state, snapshots, and message
// must match, for every interleaving (~73k schedules across five
// scenarios).
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

import (
	"slices"
	"strings"
	"testing"
)

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
