package hc

// Benchmarks for the WAL (event) state machine itself — the append
// paths, the arming protocol (amendment 1), sealing, straggler no-ops
// (amendment 20), the watchdog snapshot, and pool recycling. The
// lifecycle-level gates live in bench_test.go and the benches module;
// these isolate the wal.go primitives so the armed protocol that v1.1's
// watchdog will exercise has a perf record before it ships. (The
// owner's post-seal appends have no primitive since the modernize
// pass: annotatePostSeal appends directly under one bracketed lock.)

import (
	"errors"
	"testing"
)

// benchPre is the round-robin pool size for the rebuild pattern (same
// shape as BenchmarkEndDropPath): mutations that grow fields or flip
// one-shot state run against pre-built events, rebuilt with the timer
// stopped, so the timed region is only the operation under test.
const benchPre = 4096

func benchField() Field { return fieldStr("user_id", "u_8472") }

// BenchmarkEventAppendLive is the unarmed fast path: one atomic load,
// one slice append (amendment 1's ~1 ns budget line).
func BenchmarkEventAppendLive(b *testing.B) {
	ev := newEvent()
	ev.fields = make([]Field, 0, benchPre)
	gen := ev.state.Load() >> walStateBits
	f := benchField()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			ev.fields = ev.fields[:0]
			b.StartTimer()
		}
		ev.append(gen, f)
	}
	ev.release()
}

// BenchmarkEventAppendArmed is the guarded path: the same append with
// the event armed — mutex round trip per field. The armed/live ratio is
// the cost the v1.1 watchdog imposes on stalled requests.
func BenchmarkEventAppendArmed(b *testing.B) {
	ev := newEvent()
	ev.arm(genOf(ev))
	ev.fields = make([]Field, 0, benchPre)
	gen := ev.state.Load() >> walStateBits
	f := benchField()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			ev.fields = ev.fields[:0]
			b.StartTimer()
		}
		ev.append(gen, f)
	}
	ev.release()
}

// BenchmarkEventAppendStaleGen is the straggler no-op for a recycled
// event: generation mismatch, one load and return (amendment 20).
func BenchmarkEventAppendStaleGen(b *testing.B) {
	ev := newEvent()
	gen := ev.state.Load() >> walStateBits
	ev.reset() // bump the generation: the held gen is now stale
	f := benchField()
	b.ReportAllocs()
	for b.Loop() {
		ev.append(gen, f)
	}
	ev.release()
}

// BenchmarkEventAppendSealed is the straggler no-op for a sealed event:
// generation matches, state says sealed — the write must not land.
func BenchmarkEventAppendSealed(b *testing.B) {
	ev := newEvent()
	gen := ev.state.Load() >> walStateBits
	ev.seal()
	f := benchField()
	b.ReportAllocs()
	for b.Loop() {
		ev.append(gen, f)
	}
	ev.release()
}

// BenchmarkEventArm is the live→armed transition (watchdog arming).
func BenchmarkEventArm(b *testing.B) {
	events := make([]*event, benchPre)
	rebuild := func() {
		for i := range events {
			events[i] = newEvent()
		}
	}
	rebuild()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			rebuild()
			b.StartTimer()
		}
		e := events[n&(benchPre-1)]
		e.arm(genOf(e))
	}
	for _, ev := range events {
		ev.release()
	}
}

// BenchmarkEventSealLive is the unarmed seal: one CAS, no mutex.
func BenchmarkEventSealLive(b *testing.B) {
	events := make([]*event, benchPre)
	rebuild := func() {
		for i := range events {
			events[i] = newEvent()
		}
	}
	rebuild()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			rebuild()
			b.StartTimer()
		}
		events[n&(benchPre-1)].seal()
	}
	for _, ev := range events {
		ev.release()
	}
}

// BenchmarkEventSealArmed is the armed seal: lock, store SealedArmed,
// unlock — the path a watchdog-stalled request takes through End.
func BenchmarkEventSealArmed(b *testing.B) {
	events := make([]*event, benchPre)
	rebuild := func() {
		for i := range events {
			ev := newEvent()
			ev.arm(genOf(ev))
			events[i] = ev
		}
	}
	rebuild()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			rebuild()
			b.StartTimer()
		}
		events[n&(benchPre-1)].seal()
	}
	for _, ev := range events {
		ev.release()
	}
}

// BenchmarkEventSnapshotFields is the watchdog's read of an armed WAL:
// copy of the current tail under the append mutex.
func BenchmarkEventSnapshotFields(b *testing.B) {
	ev := newEvent()
	ev.arm(genOf(ev))
	gen := ev.state.Load() >> walStateBits
	for i := 0; i < 12; i++ {
		ev.append(gen, fieldStr("k", "v"))
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = ev.snapshotFields(genOf(ev))
	}
	ev.release()
}

// BenchmarkEventSetError is the failure-path setter on a live event:
// structuredErrorField builds the map (message, type, cause) that every
// failing request pays at End.
func BenchmarkEventSetError(b *testing.B) {
	ev := newEvent()
	ev.fields = make([]Field, 0, benchPre)
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	err := errors.New("bench failure")
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(benchPre-1) == 0 && n > 0 {
			b.StopTimer()
			ev.fields = ev.fields[:0]
			b.StartTimer()
		}
		ev.setError(ref, err)
	}
	ev.release()
}

// BenchmarkEventSetMessage is the message override on a live event.
func BenchmarkEventSetMessage(b *testing.B) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	b.ReportAllocs()
	for b.Loop() {
		ev.setMessage(ref, "override")
	}
	ev.release()
}

// BenchmarkEventSetLevel is the requested-level floor on a live event —
// the per-request write the middleware shapes make.
func BenchmarkEventSetLevel(b *testing.B) {
	ev := newEvent()
	ref := &walRef{ev: ev, gen: ev.state.Load() >> walStateBits}
	b.ReportAllocs()
	for b.Loop() {
		ev.setLevel(ref, LevelWarn)
	}
	ev.release()
}

// BenchmarkEventReleaseRecycle is the pool round trip End performs:
// release (seal + Put) then newEvent (Get + reset, one time.Now).
func BenchmarkEventReleaseRecycle(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ev := newEvent()
		ev.release()
	}
}
