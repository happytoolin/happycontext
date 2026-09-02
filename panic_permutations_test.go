package hc

// P5 panic-value permutations (action plan P5): every panic payload
// shape through every panic source, asserting the event captures the
// panic (structured panic field with the documented {type, value}
// shape), the error field falls back to "panic: <value>", the level is
// ERROR via the zero-policy panic level, and the re-panic propagates
// the original value. panic(nil) is special on Go ≥ 1.21: recover
// yields *runtime.PanicNilError, which is what the event and the
// re-panic carry.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// panicPayload is one panic value shape.
type panicPayload struct {
	name  string
	value any
}

// typedNilErr is a typed-nil panic payload (the interface is non-nil,
// the pointer is nil — the classic typed-nil trap).
type typedNilErr struct{}

func (p *typedNilErr) Error() string { return "typed-nil" }

// runtimeStyleError implements runtime.Error (a real runtime panic
// payload shape without reaching into the runtime package).
type runtimeStyleError string

func (e runtimeStyleError) Error() string        { return string(e) }
func (e runtimeStyleError) RuntimeError() string { return string(e) }

func panicPayloads() []panicPayload {
	return []panicPayload{
		{"string", "boom"},
		{"error", errors.New("boom-err")},
		{"int", 42},
		{"struct", struct{ X int }{7}},
		{"nil-interface", nil}, // panic(nil): *runtime.PanicNilError
		{"typed-nil", (*typedNilErr)(nil)},
		{"runtime-error", runtimeStyleError("runtime-style")},
	}
}

// runPanic executes fn inside a recover and returns what escaped.
func runPanic(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// capturePanicEvent runs a panicking lifecycle and returns the capture
// plus the re-panicked value.
func capturePanicEvent(t *testing.T, run func(op *Operation)) (lifeCapture, any) {
	t.Helper()
	sink := &lifeSink{}
	cfg := rtConfigFor(sink)
	op := Start(context.Background(), MustCompile(cfg), OperationStart{Domain: DomainJob, Name: "panic"})
	var rePanicked any
	func() {
		defer func() { rePanicked = recover() }()
		run(op)
	}()
	if len(sink.events) != 1 {
		t.Fatalf("captured %d events, want 1", len(sink.events))
	}
	return sink.events[0], rePanicked
}

func rtConfigFor(s Sink) Config { return Config{Sink: s, SamplingRate: 1} }

// TestPanicValuePermutations covers every payload through the direct-
// defer source (the documented usage) and the deferred-function source.
func TestPanicValuePermutations(t *testing.T) {
	for _, p := range panicPayloads() {
		p := p
		t.Run("direct-"+p.name, func(t *testing.T) {
			ev, rePanicked := capturePanicEvent(t, func(op *Operation) {
				defer op.End(nil) // direct defer: observes the panic
				panic(p.value)
			})
			verifyPanicEvent(t, ev, rePanicked, p.value, nil)
		})
		t.Run("deferred-func-"+p.name, func(t *testing.T) {
			ev, rePanicked := capturePanicEvent(t, func(op *Operation) {
				defer op.End(nil) // registered first → runs last, sees the panic
				defer func() { panic(p.value) }()
				Add(op.Context(), "before", "work")
			})
			verifyPanicEvent(t, ev, rePanicked, p.value, nil)
		})
		t.Run("co-delivered-"+p.name, func(t *testing.T) {
			err := errors.New("co-err")
			ev, rePanicked := capturePanicEvent(t, func(op *Operation) {
				defer op.End(&err) // error pointer set when the panic hits
				panic(p.value)
			})
			verifyPanicEvent(t, ev, rePanicked, p.value, err)
		})
	}
}

// verifyPanicEvent checks the captured event and the re-panicked value
// against the payload. coErr nil means the error field must carry the
// synthetic "panic: <value>" error; non-nil means the real error.
func verifyPanicEvent(t *testing.T, ev lifeCapture, rePanicked, payload any, coErr error) {
	t.Helper()

	// The re-panic propagates the original value — except panic(nil),
	// which Go ≥1.21 surfaces as *runtime.PanicNilError.
	if payload == nil {
		if _, ok := rePanicked.(error); !ok {
			t.Fatalf("panic(nil) re-panicked %T (%v), want an error", rePanicked, rePanicked)
		}
	} else if rePanicked != payload {
		t.Fatalf("re-panicked %#v, want the original %#v", rePanicked, payload)
	}

	if ev.level != LevelError {
		t.Fatalf("level = %v, want ERROR (panic policy)", ev.level)
	}

	// Panic field shape: {type, value} derived from the payload. For
	// panic(nil) the observed value is the *runtime.PanicNilError
	// wrapper Go ≥1.21 recovers, so expectations derive from it.
	observed := payload
	if payload == nil {
		observed = rePanicked
	}
	line := string(ev.line)
	panicField, ok := eventMember(t, ev, "panic")
	if !ok {
		t.Fatalf("missing panic field in %s", line)
	}
	wantType := fmt.Sprintf("%T", observed)
	if got := panicField["type"]; got != wantType {
		t.Fatalf("panic.type = %v, want %s", got, wantType)
	}
	wantValue := fmt.Sprint(observed)
	if got := panicField["value"]; got != wantValue {
		t.Fatalf("panic.value = %q, want %q", got, wantValue)
	}

	// Error field: the real co-delivered error or the synthetic panic
	// fallback.
	ef, ok := eventMember(t, ev, "error")
	if !ok {
		t.Fatalf("missing error field in %s", line)
	}
	if coErr != nil {
		if ef["message"] != coErr.Error() {
			t.Fatalf("error.message = %v, want %q", ef["message"], coErr.Error())
		}
	} else {
		want := "panic: " + fmt.Sprint(observed)
		if ef["message"] != want {
			t.Fatalf("error.message = %v, want %q", ef["message"], want)
		}
	}
}

// eventMember parses the captured line and returns one member's value
// as a decoded map.
func eventMember(t *testing.T, ev lifeCapture, key string) (map[string]any, bool) {
	t.Helper()
	got, _ := decodeLineStrict(t, ev.line)
	raw, ok := got[key]
	if !ok {
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("member %s not an object: %v", key, err)
	}
	return m, true
}

// TestSinkPanicPermutations: a panicking sink (payload from the table)
// with and without an in-flight handler panic. The sink panic replaces
// the in-flight panic (documented) and must not corrupt the pool.
func TestSinkPanicPermutations(t *testing.T) {
	for _, p := range panicPayloads() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			sink := &captureThenPanicSink{payload: p.value}
			rt := MustCompile(Config{Sink: sink, SamplingRate: 1})
			op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "sink-panic"})
			Add(op.Context(), "k", "v")

			var escaped any
			func() {
				defer func() { escaped = recover() }()
				op.End(nil)
			}()
			if escaped == nil {
				t.Fatal("sink panic did not escape End")
			}
			if p.value == nil {
				// sink panics with panic(nil) → PanicNilError wrapper
				if _, ok := escaped.(error); !ok {
					t.Fatalf("escaped %T", escaped)
				}
			} else if escaped != p.value {
				t.Fatalf("escaped %#v, want sink payload %#v", escaped, p.value)
			}
			if len(sink.captured) != 1 {
				t.Fatalf("sink captured %d records", len(sink.captured))
			}
			// pool integrity after the sink panic
			ok := &matrixSink{}
			rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
			op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
			Add(op2.Context(), "clean", true)
			op2.End(nil)
			if len(ok.capture) != 1 || !bytes.Contains(ok.capture[0], []byte(`"clean":true`)) {
				t.Fatalf("pool corrupted after sink panic: %v", ok.capture)
			}
		})
	}
}

// captureThenPanicSink records the record, then panics with the
// configured payload.
type captureThenPanicSink struct {
	payload  any
	captured [][]byte
}

func (s *captureThenPanicSink) Write(_ context.Context, rec *Record) {
	s.captured = append(s.captured, bytes.Clone(rec.Encoded()))
	panic(s.payload)
}

// TestPanicAfterEndRan documents the defer-order requirement: when End
// is deferred LAST it runs first — a panic in a later deferred
// function is NOT captured (the event commits as a success) and the
// panic propagates.
func TestPanicAfterEndRan(t *testing.T) {
	sink := &lifeSink{}
	op := Start(context.Background(), MustCompile(rtConfigFor(sink)), OperationStart{Domain: DomainJob, Name: "late"})
	var escaped any
	func() {
		defer func() { escaped = recover() }()
		defer func() { panic("late-panic") }() // runs first
		defer op.End(nil)                      // runs last: no panic in flight
	}()
	if escaped != "late-panic" {
		t.Fatalf("escaped %v", escaped)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d", len(sink.events))
	}
	if v := sink.events[0].level; v != LevelInfo {
		t.Fatalf("level = %v, want INFO (End ran before the panic)", v)
	}
	if got := sink.events[0].line; bytes.Contains(got, []byte(`"panic"`)) {
		t.Fatalf("late panic leaked into the event: %s", got)
	}
}
