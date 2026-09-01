package hc

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

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
