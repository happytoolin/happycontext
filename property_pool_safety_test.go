package hc

// P6d property (dst-research §7.6, action plan P6): pool safety —
// post-End immutability under straggler replay. After request 1 ends
// and a subsequent request completes, replaying ANY prefix of request
// 1's write program through the stale context mutates neither the
// first event (already captured) nor the second event's
// fields/outcome/level/message: the sealed event drops every straggler
// write at the generation/state check (amendments 1/20), whether or
// not the pool recycled the event into request 2.
//
// The oracle is white-box state comparison of the second request's
// event plus capture equality for both events: the replay is applied
// only after both requests completed, so any mutation visible here
// means a sealed write landed.

import (
	"bytes"
	"context"
	"math/rand/v2"
	"reflect"
	"testing"
)

// poolState snapshots everything a straggler write could mutate on a
// sealed event: the WAL, the message, the requested level, and the
// error latch.
type poolState struct {
	fields      []Field
	msg         string
	hasMsg      bool
	level       Level
	hasLevel    bool
	hasErr      bool
	sealed      bool
	sealedArmed bool
}

func snapshotState(ev *event) poolState {
	s := ev.state.Load()
	state := walState(s & walStateMask)
	return poolState{
		fields:      append([]Field(nil), ev.fields...),
		msg:         ev.msg,
		hasMsg:      ev.hasMsg,
		level:       ev.requestedLevel,
		hasLevel:    ev.hasRequestedLvl,
		hasErr:      ev.hasErr,
		sealed:      state == walSealed,
		sealedArmed: state == walSealedArmed,
	}
}

func statesEqual(a, b poolState) bool {
	return a.msg == b.msg && a.hasMsg == b.hasMsg && a.level == b.level &&
		a.hasLevel == b.hasLevel && a.hasErr == b.hasErr &&
		a.sealed == b.sealed && a.sealedArmed == b.sealedArmed &&
		reflect.DeepEqual(a.fields, b.fields)
}

// capturedEqual compares two captured events (used to pin that the
// records handed to sinks are immutable snapshots).
func capturedEqual(a, b lifeCapture) bool {
	return a.level == b.level && a.message == b.message && bytes.Equal(a.line, b.line)
}

// TestPoolSafetyReplayProperty replays generated program prefixes
// through a stale context after both requests completed.
func TestPoolSafetyReplayProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x90F15E5eed, 0xE6A1E5eed))
	for i := 0; i < 400; i++ {
		buf := make([]byte, 2+rng.IntN(160))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate1 // pool safety needs live emissions

		// Request 1 runs the program on its own runtime.
		sink1 := &lifeSink{}
		rt1 := MustCompile(Config{Sink: sink1, SamplingRate: 1})
		op1 := Start(context.Background(), rt1, OperationStart{Domain: prog.start, Name: "one"})
		executeProgramOn(prog, op1) // ctx1 := op1.Context() is the stale handle replayed below
		if len(sink1.events) != 1 {
			continue // dropped or never-ended: nothing to protect
		}
		firstCapture := sink1.events[0]

		// Request 2 completes on the same pool.
		sink2 := &lifeSink{}
		rt2 := MustCompile(Config{Sink: sink2, SamplingRate: 1})
		op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "two"})
		Add(op2.Context(), "second", "owned")
		op2.End(nil)
		if len(sink2.events) != 1 {
			t.Fatalf("iter %d: request 2 emitted %d events", i, len(sink2.events))
		}
		secondCapture := sink2.events[0]
		ev2 := op2.ev
		before2 := snapshotState(ev2)

		// Replay every prefix of the first program through the stale
		// context, including the empty prefix and the full program.
		for k := 0; k <= len(prog.ops); k++ {
			prefix := lifeProgram{mode: prog.mode, start: prog.start, ops: prog.ops[:k]}
			executeProgramOn(prefix, op1) // stale ctx writes: must all no-op
		}

		if !statesEqual(snapshotState(ev2), before2) {
			t.Fatalf("iter %d: replay through the stale context mutated request 2's event", i)
		}
		if !capturedEqual(sink2.events[0], secondCapture) {
			t.Fatalf("iter %d: replay corrupted the captured second event", i)
		}
		if !capturedEqual(sink1.events[0], firstCapture) {
			t.Fatalf("iter %d: replay corrupted the captured first event", i)
		}
	}
}
