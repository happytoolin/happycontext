package hc

// Agent E — configuration and API misuse: hostile rates, post-Compile
// mutation of Config maps, nil receivers on every exported entry
// point, malformed Add tails, and garbage setter arguments.

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestCrashHostileRates(t *testing.T) {
	bad := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.1, 1.0000001, 2, -42}
	for _, r := range bad {
		if _, err := Compile(Config{SamplingRate: r}); err == nil {
			t.Fatalf("rate %v accepted", r)
		}
	}
	good := []float64{0, 1, 0.5, -0.0}
	for _, r := range good {
		if _, err := Compile(Config{SamplingRate: r}); err != nil {
			t.Fatalf("rate %v rejected: %v", r, err)
		}
	}
	// Hostile values nested in the maps too.
	if _, err := Compile(Config{LevelSamplingRates: map[Level]float64{LevelInfo: math.NaN()}}); err == nil {
		t.Fatal("NaN level rate accepted")
	}
	inf := math.Inf(1)
	if _, err := Compile(Config{OperationPolicies: map[Domain]OperationPolicy{"job": {SamplingRate: &inf}}}); err == nil {
		t.Fatal("+Inf policy rate accepted")
	}
}

// The compiled Runtime shares no mutable state with the caller's
// Config: mutating every map (and rate pointers) after Compile must
// not change behavior.
func TestCrashConfigMutatedAfterCompile(t *testing.T) {
	levelRates := map[Level]float64{LevelInfo: 1.0}
	polRate := 0.0
	cfg := Config{
		Sink:               NewTestSink(),
		SamplingRate:       1.0,
		LevelSamplingRates: levelRates,
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {SamplingRate: &polRate, OutcomeLevels: map[Outcome]Level{OutcomeSuccess: LevelInfo}},
		},
	}
	rt, err := Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Hostile post-compile mutation.
	levelRates[LevelInfo] = -5
	polRate = 7
	cfg.OperationPolicies[DomainJob].OutcomeLevels[OutcomeSuccess] = Level(-99)
	cfg.SamplingRate = -1

	// Job domain keeps its policy (rate ptr cloned at 0.0): dropped.
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	if op.End(nil) {
		t.Fatal("mutated policy leaked into compiled runtime (job should drop at rate 0)")
	}
	// Default domain unaffected by the job policy: rate 1 keeps.
	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainCLI, Name: "c"})
	if !op2.End(nil) {
		t.Fatal("default domain dropped after config mutation")
	}
}

func TestCrashNilReceiversAndArgs(t *testing.T) {
	// Operations.
	var nilOp *Operation
	if nilOp.End(nil) {
		t.Fatal("nil op End emitted")
	}
	if nilOp.Context() != nil {
		t.Fatal("nil op Context not nil")
	}
	// Start with nil runtime and nil ctx.
	op := Start(nil, nil, OperationStart{})
	if op.End(nil) {
		t.Fatal("nil runtime emitted")
	}
	// Context helpers with nil/no-event ctx: silent no-ops.
	Add(nil, "k", "v")
	Add(context.Background(), "k", "v")
	AddRawJSON(nil, "k", []byte(`{}`))
	AddRawJSON(context.Background(), "k", nil)
	Error(nil, nil)
	Error(context.Background(), nil)
	SetMessage(nil, "")
	SetRoute(nil, "")
	SetLevel(nil, Level(999))
	// Sinks.
	(*JSONSink)(nil).Write(context.Background(), nil)
	NewJSONSink(nil).Write(context.Background(), nil)
	var nilTest *TestSink
	nilTest.Write(context.Background(), nil)
	if len(nilTest.Events()) != 0 {
		t.Fatal("nil TestSink has events")
	}
	nilTest.Reset()
	// Record accessors on zero records.
	var zeroRec Record
	_ = zeroRec.Encoded()
	_ = zeroRec.Fields()
	_, _ = zeroRec.Lookup("k")
	var zeroField Field
	_ = zeroField.Key()
	_ = zeroField.WireKey()
	// MustCompile panics loudly on garbage.
	func() {
		defer func() { _ = recover() }()
		MustCompile(Config{SamplingRate: math.NaN()})
		t.Fatal("MustCompile accepted NaN")
	}()
}

// Malformed variadic tails: non-string keys, empty keys, odd tails —
// skipped silently, well-formed pairs kept.
func TestCrashAddMalformedTails(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(),
		"good", 1,
		42, "non-string key",
		"", "empty key",
		"also_good", 2,
		"dangling",
	)
	_ = op.End(nil)
	ev := ts.Events()[0]
	good, _ := ev.Lookup("good")
	if v, _ := good.(int64); v != 1 {
		t.Fatalf("good = %#v", good)
	}
	also, _ := ev.Lookup("also_good")
	if v, _ := also.(int64); v != 2 {
		t.Fatalf("also_good = %#v", also)
	}
	if _, ok := ev.Lookup("dangling"); ok {
		t.Fatal("dangling tail became a field")
	}
	if n := len(ev.Fields()); n < 2 {
		t.Fatalf("fields = %d", n)
	}
}

func TestCrashSetterGarbage(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	SetLevel(op.Context(), LevelWarn)  // floor can only raise severity
	SetLevel(op.Context(), Level(100)) // invalid: ignored
	SetRoute(op.Context(), "")
	SetMessage(op.Context(), "")
	Add(op.Context(), "took", -1*time.Hour) // negative duration
	_ = op.End(nil)
	ev := ts.Events()[0]
	if ev.Level() != LevelWarn {
		t.Fatalf("level = %v, want warn (floor raised)", ev.Level())
	}
	if _, ok := ev.Lookup("http.route"); ok {
		t.Fatal("empty route became a field")
	}
}
