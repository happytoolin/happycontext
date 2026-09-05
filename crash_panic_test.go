package hc

// Agent A — panic torture. Charters gaps the existing permutation
// suites leave open: panics from inside the commit pipeline itself
// (sampler, encoder via user MarshalJSON), panic(nil), the closure
// form of End, double-End after a panic, and pool integrity after
// every crash shape.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// panickingSampler explodes during commit — inside End, after seal.
func TestCrashPanicInSamplerDuringCommit(t *testing.T) {
	rt, _ := testRT(t, func(c *Config) {
		c.Sampler = func(in SampleInput) bool { panic("sampler boom") }
	})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "k", "v")

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("sampler panic must propagate out of End")
			}
		}()
		_ = op.End(nil)
	}()

	// Second End is the published no-op, and the pool still works.
	if op.End(nil) {
		t.Fatal("second End after crash must report not-emitted")
	}
	rt2, ts2 := testRT(t, nil)
	op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "j2"})
	Add(op2.Context(), "after", "crash")
	if !op2.End(nil) || len(ts2.Events()) != 1 {
		t.Fatal("pool unusable after sampler panic")
	}
	if v, _ := ts2.Events()[0].Lookup("after"); v != "crash" {
		t.Fatalf("post-crash event corrupted: %v", v)
	}
}

// boomMarshal panics inside MarshalJSON. encoding/json contains
// marshaler panics as errors, so the pipeline must survive: the event
// is emitted with a marshaling-error string instead of crashing.
type boomMarshal struct{}

func (boomMarshal) MarshalJSON() ([]byte, error) { panic("marshal boom") }

// errMarshal errors inside MarshalJSON — must become a string field,
// never a dropped event or a broken line.
type errMarshal struct{}

func (errMarshal) MarshalJSON() ([]byte, error) { return nil, errors.New("nope") }

func TestCrashMarshalJSONPanicContained(t *testing.T) {
	// The panic fires inside Encoded(), so the sink must encode:
	// JSONSink serves the canonical line on Write. encoding/json
	// recovers marshaler panics as errors — no panic may escape End,
	// and the line must stay parseable with the error as a string.
	var buf strings.Builder
	rt := MustCompile(Config{Sink: NewJSONSink(&buf), SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "boom", boomMarshal{}, "ok", 1)

	emitted := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("marshaler panic escaped End: %v", r)
			}
		}()
		emitted = op.End(nil)
	}()
	if !emitted {
		t.Fatal("event dropped on marshaler panic")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(buf.String()), &m); err != nil {
		t.Fatalf("line not parseable: %v: %s", err, buf.String())
	}
	if s, _ := m["boom"].(string); !strings.Contains(s, "marshaling error") || !strings.Contains(s, "boom") {
		t.Fatalf("boom = %#v, want contained marshaler-panic error", m["boom"])
	}
	if m["ok"] != float64(1) {
		t.Fatalf("sibling field lost: %#v", m["ok"])
	}

	// The pool must serve the next request untouched.
	rt2, ts2 := testRT(t, nil)
	op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "j2"})
	Add(op2.Context(), "ok", 1)
	if !op2.End(nil) || len(ts2.Events()) != 1 {
		t.Fatal("pool unusable after marshaler panic")
	}
}

func TestCrashMarshalErrorBecomesStringField(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "bad", errMarshal{})
	if !op.End(nil) || len(ts.Events()) != 1 {
		t.Fatal("event dropped on marshal error")
	}
	if _, ok := ts.Events()[0].Lookup("bad"); !ok {
		t.Fatal("field dropped from capture")
	}
	// TestSink never marshals, so assert on the encoded line: the
	// error becomes a marshaling-error string and the line stays
	// parseable JSON.
	line := recOf(LevelInfo, "m", fieldOf("bad", errMarshal{})).Encoded()
	m := map[string]any{}
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("line not parseable: %v: %s", err, line)
	}
	s, _ := m["bad"].(string)
	if !strings.Contains(s, "marshaling error") {
		t.Fatalf("bad = %#v, want marshaling-error string", m["bad"])
	}
}

// panic(nil) becomes *runtime.PanicNilError on go >= 1.21: the event
// must still carry a structured panic field and outcome=panic.
func TestCrashPanicNilValue(t *testing.T) {
	rt, ts := testRT(t, nil)
	func() {
		defer func() { _ = recover() }()
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		defer op.End(nil)
		panic(nil)
	}()
	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if o, _ := evs[0].Lookup("op.outcome"); o != string(OutcomePanic) {
		t.Fatalf("outcome = %v, want panic", o)
	}
	if _, ok := evs[0].Lookup("panic"); !ok {
		t.Fatal("panic field missing for panic(nil)")
	}
}

// The documented footgun: End called from inside a closure loses
// panic capture. Pin the exact behavior — the panic escapes and End
// has already committed a success-shaped event.
func TestCrashEndFromClosureLosesPanicCapture(t *testing.T) {
	rt, ts := testRT(t, nil)
	func() {
		defer func() { _ = recover() }()
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		defer func() { _ = op.End(nil) }() // closure form: no capture
		panic("escaped")
	}()
	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1 (committed as non-panic)", len(evs))
	}
	if o, _ := evs[0].Lookup("op.outcome"); o != string(OutcomeSuccess) {
		t.Fatalf("closure End outcome = %v, want success (capture disabled)", o)
	}
}

// End after a caught panic-repanic cycle is a published no-op.
func TestCrashDoubleEndAfterPanic(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	func() {
		defer func() { _ = recover() }()
		defer op.End(nil)
		panic("first")
	}()
	if second := op.End(nil); !second {
		t.Fatal("one-shot second End must return the first result (emitted)")
	}
	if len(ts.Events()) != 1 {
		t.Fatalf("events = %d, want 1", len(ts.Events()))
	}
}

// A panicking sink must not poison the pool for later requests, even
// when a straggler write lands between the crash and the next start.
func TestCrashPanicInSinkWithStragglerAftermath(t *testing.T) {
	rt := MustCompile(Config{Sink: crashPanicSink{}, SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "k", "v")
	func() {
		defer func() { _ = recover() }()
		_ = op.End(nil)
	}()
	// Straggler after the crashed (and released) event: silent no-op.
	Add(op.Context(), "straggler", true)

	rt2, ts2 := testRT(t, nil)
	op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "j2"})
	Add(op2.Context(), "clean", true)
	if !op2.End(nil) || len(ts2.Events()) != 1 {
		t.Fatal("pool unusable after sink panic + straggler")
	}
	if v, _ := ts2.Events()[0].Lookup("clean"); v != true {
		t.Fatalf("post-crash event corrupted: %v", v)
	}
}

type crashPanicSink struct{}

func (crashPanicSink) Write(context.Context, *Record) { panic("sink boom") }

// Nested operations: an inner op committing during an outer panic
// storm stays isolated (context shadowing is innermost-wins).
func TestCrashNestedOpsDuringOuterPanic(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	Add(op.Context(), "outer", 1)

	func() {
		defer func() { _ = recover() }()
		inner := Start(op.Context(), rt, OperationStart{Domain: DomainJob, Name: "inner"})
		Add(inner.Context(), "inner", 2)
		defer inner.End(nil)
		panic("outer handler panic")
	}()

	_ = op.End(nil)
	evs := ts.Events()
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2 (inner + outer)", len(evs))
	}
	var innerSeen, outerSeen bool
	for _, ev := range evs {
		if _, ok := ev.Lookup("inner"); ok {
			innerSeen = true
			// Defer LIFO: the inner End is the innermost frame on the
			// panic path, so IT captures the panic and re-panics.
			if o, _ := ev.Lookup("op.outcome"); o != string(OutcomePanic) {
				t.Fatalf("inner outcome = %v, want panic (innermost defer captures)", o)
			}
		}
		if _, ok := ev.Lookup("outer"); ok {
			outerSeen = true
			// The re-panic was consumed by the test's recover before the
			// outer End ran: it commits clean.
			if o, _ := ev.Lookup("op.outcome"); o != string(OutcomeSuccess) {
				t.Fatalf("outer outcome = %v, want success (panic already consumed)", o)
			}
		}
	}
	if !innerSeen || !outerSeen {
		t.Fatalf("inner=%v outer=%v", innerSeen, outerSeen)
	}
}
