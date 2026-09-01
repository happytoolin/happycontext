package hc

import (
	"errors"
	"math"
	"testing"
)

func TestCompileValid(t *testing.T) {
	rate := 0.25
	rt, err := Compile(Config{
		Sink:               NewTestSink(),
		SamplingRate:       1,
		LevelSamplingRates: map[Level]float64{LevelDebug: 0.1},
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {SuccessLevel: LevelDebug, SamplingRate: &rate},
		},
		Message: "done",
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if rt.noop() {
		t.Fatal("valid sink compiled to noop")
	}
	if rt.message != "done" {
		t.Errorf("message = %q", rt.message)
	}
}

func TestCompileNilSinkIsValidNoop(t *testing.T) {
	rt, err := Compile(Config{})
	if err != nil {
		t.Fatalf("nil sink must be valid: %v", err)
	}
	if !rt.noop() {
		t.Fatal("nil sink must be a no-op runtime")
	}
}

func TestCompileSentinels(t *testing.T) {
	cases := []struct {
		name  string
		cfg   Config
		sent  error
		inMsg string
	}{
		{
			name:  "rate above one",
			cfg:   Config{SamplingRate: 1.5},
			sent:  ErrInvalidRate,
			inMsg: "hc: sampling rate 1.5",
		},
		{
			name:  "rate negative",
			cfg:   Config{SamplingRate: -0.1},
			sent:  ErrInvalidRate,
			inMsg: "hc: sampling rate -0.1",
		},
		{
			name: "rate NaN",
			cfg:  Config{SamplingRate: math.NaN()},
			sent: ErrInvalidRate,
		},
		{
			name: "level rate bad level key",
			cfg:  Config{LevelSamplingRates: map[Level]float64{Level(3): 0.5}},
			sent: ErrInvalidLevel,
		},
		{
			name: "level rate bad rate",
			cfg:  Config{LevelSamplingRates: map[Level]float64{LevelWarn: 2}},
			sent: ErrInvalidRate,
		},
		{
			name: "policy bad success level",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {SuccessLevel: Level(7)}}},
			sent: ErrInvalidLevel,
		},
		{
			name: "policy bad outcome key",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {OutcomeLevels: map[Outcome]Level{Outcome("nope"): LevelWarn}}}},
			sent: ErrInvalidOutcome,
		},
		{
			name: "policy bad rate",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {SamplingRate: floatPtr(1.2)}}},
			sent: ErrInvalidRate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Compile(c.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, c.sent) {
				t.Fatalf("error %v does not wrap sentinel %v", err, c.sent)
			}
			if c.inMsg != "" && err.Error()[:len(c.inMsg)] != c.inMsg {
				t.Fatalf("error %q lacks %q prefix", err, c.inMsg)
			}
		})
	}
}

func TestMustCompilePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustCompile should panic on bad config")
		}
	}()
	MustCompile(Config{SamplingRate: 2})
}

func TestMustCompileLiteralOK(t *testing.T) {
	rt := MustCompile(Config{Sink: NewTestSink(), SamplingRate: 1})
	if rt == nil {
		t.Fatal("nil runtime")
	}
}

func TestLevelString(t *testing.T) {
	cases := map[Level]string{
		LevelDebug: "DEBUG",
		LevelInfo:  "INFO",
		LevelWarn:  "WARN",
		LevelError: "ERROR",
		Level(42):  "INFO", // unknown renders as info, wire unchanged
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", int(level), got, want)
		}
	}
}

// TestLevelConstantCompatibility pins the slog ranks so v0 source that
// references the constants keeps compiling and behaving identically.
func TestLevelConstantCompatibility(t *testing.T) {
	if LevelDebug != -4 || LevelInfo != 0 || LevelWarn != 4 || LevelError != 8 {
		t.Fatal("level ranks drifted from the slog-compatible values")
	}
	if !IsValidLevel(LevelWarn) || IsValidLevel(Level(3)) {
		t.Fatal("IsValidLevel misbehaves")
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestPolicyForDomain(t *testing.T) {
	rt := MustCompile(Config{
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {SuccessLevel: LevelDebug},
		},
	})
	if p := rt.policyFor(DomainJob); p.SuccessLevel != LevelDebug {
		t.Errorf("job policy = %+v", p)
	}
	if p := rt.policyFor(DomainHTTP); p.SuccessLevel != LevelInfo {
		t.Errorf("unconfigured policy not zero: %+v", p)
	}
	// empty-string domain normalizes to the default domain
	rt2 := MustCompile(Config{
		OperationPolicies: map[Domain]OperationPolicy{"": {SuccessLevel: LevelWarn}},
	})
	if p := rt2.policyFor(Domain("")); p.SuccessLevel != LevelWarn {
		t.Errorf("alias policy = %+v", p)
	}
}
