package hc

// FuzzCompileConfig (P3, dst-research §6.3) fuzzes hc.Compile with
// extreme configurations: NaN/±Inf/out-of-range rates, empty and
// enormous policy maps, invalid levels and outcomes, nil pointers,
// weird domains. The oracle checks the documented contract:
//
//  1. Compile never panics.
//  2. err != nil ⇒ the error wraps one of the three sentinels
//     (errors.Is works) with the "hc: " prefix.
//  3. err == nil ⇒ the runtime is usable end-to-end (a full request
//     runs against a TestSink) and immutable: mutating every field of
//     the caller's Config after Compile changes nothing the runtime
//     observes.
//
// Accepted rates are exactly [0,1]: NaN and friends must be rejected —
// the negated-range idiom in Compile (`!(rate >= 0 && rate <= 1)`)
// makes NaN fall into the error branch, which this target pins.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

// compileRate draws a hostile rate from a byte: NaN, ±Inf, denormals,
// out-of-range, and the valid edges.
func compileRate(b byte) float64 {
	switch b % 8 {
	case 0:
		return math.NaN()
	case 1:
		return math.Inf(1)
	case 2:
		return math.Inf(-1)
	case 3:
		return -1
	case 4:
		return 1.0000001
	case 5:
		return 0
	case 6:
		return 1
	default:
		return float64(b) / 255 // interior of [0,1]
	}
}

var compileLevels = []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, Level(99), Level(-100), Level(0)}

var compileDomains = []Domain{DomainHTTP, DomainJob, "", "operation", "weird domain", "http"}

var compileOutcomes = []Outcome{
	OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry, "nonsense", "",
}

// configFromBytes decodes a hostile Config from fuzz bytes.
func configFromBytes(b []byte, sink Sink) Config {
	next := func() byte {
		if len(b) == 0 {
			return 0
		}
		v := b[0]
		b = b[1:]
		return v
	}
	cfg := Config{Sink: sink, SamplingRate: compileRate(next())}

	if next()&1 != 0 {
		cfg.LevelSamplingRates = map[Level]float64{}
		for i := next() % 5; i > 0; i-- {
			cfg.LevelSamplingRates[compileLevels[int(next())%len(compileLevels)]] = compileRate(next())
		}
	}
	if next()&1 != 0 {
		cfg.Sampler = NeverSampler()
	}
	if next()&1 != 0 {
		cfg.OperationPolicies = map[Domain]OperationPolicy{}
		for i := next() % 5; i > 0; i-- {
			domain := compileDomains[int(next())%len(compileDomains)]
			policy := OperationPolicy{
				SuccessLevel: compileLevels[int(next())%len(compileLevels)],
				FailureLevel: compileLevels[int(next())%len(compileLevels)],
				PanicLevel:   compileLevels[int(next())%len(compileLevels)],
			}
			if next()&1 != 0 {
				policy.OutcomeLevels = map[Outcome]Level{}
				for j := next() % 4; j > 0; j-- {
					policy.OutcomeLevels[compileOutcomes[int(next())%len(compileOutcomes)]] = compileLevels[int(next())%len(compileLevels)]
				}
			}
			if next()&1 != 0 {
				r := compileRate(next())
				policy.SamplingRate = &r
			}
			cfg.OperationPolicies[domain] = policy
		}
	}
	if next()&1 != 0 {
		msg := b
		cfg.Message = string(msg)
	}
	return cfg
}

// configIsValid mirrors Compile's acceptance predicate independently:
// the rate ranges and the level/outcome keys. (Domain keys never fail
// validation — any string domain compiles.)
func configIsValid(cfg Config) bool {
	if !(cfg.SamplingRate >= 0 && cfg.SamplingRate <= 1) {
		return false
	}
	for level, rate := range cfg.LevelSamplingRates {
		if !IsValidLevel(level) || !(rate >= 0 && rate <= 1) {
			return false
		}
	}
	for _, policy := range cfg.OperationPolicies {
		if !IsValidLevel(policy.SuccessLevel) || !IsValidLevel(policy.FailureLevel) || !IsValidLevel(policy.PanicLevel) {
			return false
		}
		for outcome, level := range policy.OutcomeLevels {
			if !IsValidOutcome(outcome) || !IsValidLevel(level) {
				return false
			}
		}
		if policy.SamplingRate != nil {
			if r := *policy.SamplingRate; !(r >= 0 && r <= 1) {
				return false
			}
		}
	}
	return true
}

// driveOneRequest runs one full request through rt, asserting nothing
// panics and that emission is consistent with the sink. withErr makes
// the event an error (amendment-4 bypass): its emission is then
// deterministic — immune to every rate and sampler in the config.
// Returns the captured level, message, and emission for the probes.
func driveOneRequest(t *testing.T, rt *Runtime, name string, withErr bool) (level Level, msg string, emitted bool) {
	t.Helper()
	ts, _ := rt.sink.(*TestSink)
	if ts != nil {
		ts.Reset()
	}
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: name})
	Add(op.Context(), "k", "v")
	if withErr {
		err := errors.New("probe-error")
		emitted = op.End(&err)
	} else {
		emitted = op.End(nil)
	}
	if ts != nil {
		events := ts.Events()
		if emitted != (len(events) == 1) {
			t.Fatalf("emitted=%v but captured %d events", emitted, len(events))
		}
		if emitted {
			return events[0].Level(), events[0].Message(), true
		}
	} else if emitted {
		t.Fatal("runtime with a nil sink reported emission")
	}
	return 0, "", emitted
}

func FuzzCompileConfig(f *testing.F) {
	f.Add([]byte{0})                      // all defaults
	f.Add([]byte{0, 1, 5})                // NaN global rate
	f.Add([]byte{3, 255})                 // negative rate
	f.Add([]byte{4, 1, 2, 3, 4, 5, 6, 7}) // levelRates with invalid level
	f.Add([]byte{6, 0})                   // valid interior rate
	f.Fuzz(func(t *testing.T, b []byte) {
		cfg := configFromBytes(b, NewTestSink())
		rt, err := Compile(cfg)
		if err != nil {
			if configIsValid(cfg) {
				t.Fatalf("Compile rejected a valid config: %v", err)
			}
			// Sentinel contract: errors.Is works against the wrapped
			// sentinel, with the "hc: " prefix.
			sentinel := false
			for _, s := range []error{ErrInvalidRate, ErrInvalidLevel, ErrInvalidOutcome} {
				if errors.Is(err, s) {
					sentinel = true
				}
			}
			if !sentinel {
				t.Fatalf("error %q wraps no sentinel", err)
			}
			if !strings.HasPrefix(err.Error(), "hc: ") {
				t.Fatalf("error %q lacks the hc: prefix", err)
			}
			return
		}

		if !configIsValid(cfg) {
			t.Fatalf("Compile accepted an invalid config: rate=%v levels=%v policies=%v",
				cfg.SamplingRate, cfg.LevelSamplingRates, cfg.OperationPolicies)
		}

		// Probe 1 — the compiled runtime is usable end to end. An error
		// event emits deterministically (the amendment-4 bypass makes
		// emission immune to every rate and sampler in the config).
		if _, _, emitted := driveOneRequest(t, rt, "error-drive", true); !emitted {
			t.Fatal("valid runtime dropped an error event")
		}

		// Probe 2 — immutability under deterministic rates. Healthy
		// emission must be decided by rate 1 (keep), never by a coin
		// flip: the pre/post comparison below then catches ANY aliasing
		// of the caller's config — a leaked map or *float64 flips
		// emission, level, or message between the two drives (review
		// findings GLM-2 / DS-9). The hostile cfg itself was validated
		// by the Compile above; the probe clone only makes the rates
		// deterministic.
		probeCfg := cfg
		probeCfg.SamplingRate = 1
		for level := range probeCfg.LevelSamplingRates {
			probeCfg.LevelSamplingRates[level] = 1
		}
		for domain, policy := range probeCfg.OperationPolicies {
			if policy.SamplingRate != nil {
				one := 1.0
				policy.SamplingRate = &one
				probeCfg.OperationPolicies[domain] = policy
			}
		}
		rt2, err := Compile(probeCfg)
		if err != nil {
			t.Fatalf("deterministic probe config rejected: %v", err)
		}
		preLevel, preMsg, preEmitted := driveOneRequest(t, rt2, "pre-mutation", false)

		// Mutate every mutable field of the caller's config.
		probeCfg.SamplingRate = 0 // was 1: a leaked rate drops the healthy event
		probeCfg.Message = "mutated-message"
		for level := range probeCfg.LevelSamplingRates {
			probeCfg.LevelSamplingRates[level] = 0 // leaked map drops the healthy event
		}
		for domain, policy := range probeCfg.OperationPolicies {
			policy.SuccessLevel = LevelError // leaked policy raises the level
			policy.FailureLevel = LevelDebug
			if policy.OutcomeLevels != nil {
				policy.OutcomeLevels[OutcomeRetry] = LevelWarn
			}
			if policy.SamplingRate != nil {
				zero := 0.0
				policy.SamplingRate = &zero // leaked rate pointer drops the event
			}
			probeCfg.OperationPolicies[domain] = policy
		}
		postLevel, postMsg, postEmitted := driveOneRequest(t, rt2, "post-mutation", false)

		// The compiled runtime must behave identically: emission first
		// (unconditional — a leaked rate would flip it), then level and
		// message when the healthy event was emitted at all.
		if postEmitted != preEmitted {
			t.Fatalf("config mutation flipped emission: pre=%v post=%v — "+
				"the runtime aliases the caller's config", preEmitted, postEmitted)
		}
		if preEmitted {
			if postMsg != preMsg {
				t.Fatalf("runtime message changed after config mutation: %q -> %q", preMsg, postMsg)
			}
			if postLevel != preLevel {
				t.Fatalf("runtime level changed after config mutation: %v -> %v", preLevel, postLevel)
			}
		}
	})
}

// TestCompileConfigPropertyRandom drives the same oracle over
// PCG-generated configs — seed-only coverage without the fuzzer.
func TestCompileConfigPropertyRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xC0FFE5eed, 0x00F1E5))
	for i := range 2000 {
		buf := make([]byte, 1+rng.IntN(60))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		cfg := configFromBytes(buf, NewTestSink())
		rt, err := Compile(cfg)
		if err != nil {
			if configIsValid(cfg) {
				t.Fatalf("iter %d: Compile rejected valid config %+v: %v", i, cfg, err)
			}
			sentinel := errors.Is(err, ErrInvalidRate) || errors.Is(err, ErrInvalidLevel) || errors.Is(err, ErrInvalidOutcome)
			if !sentinel {
				t.Fatalf("iter %d: error %q wraps no sentinel", i, err)
			}
			continue
		}
		if !configIsValid(cfg) {
			t.Fatalf("iter %d: Compile accepted invalid config", i)
		}
		driveOneRequest(t, rt, fmt.Sprintf("iter-%d", i), true) // error drive: deterministic
	}
}
