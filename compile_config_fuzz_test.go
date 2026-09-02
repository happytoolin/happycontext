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
// panics and that emission is consistent with the sink (sampling may
// drop healthy events, so no count is asserted). Returns the event's
// message when one was emitted, for the immutability probe.
func driveOneRequest(t *testing.T, rt *Runtime, name string) (msg string, emitted bool) {
	t.Helper()
	ts, _ := rt.sink.(*TestSink)
	if ts != nil {
		ts.Reset()
	}
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: name})
	Add(op.Context(), "k", "v")
	emitted = op.End(nil)
	if ts != nil {
		events := ts.Events()
		if emitted != (len(events) == 1) {
			t.Fatalf("emitted=%v but captured %d events", emitted, len(events))
		}
		if emitted {
			return events[0].Message(), true
		}
	} else if emitted {
		t.Fatal("runtime with a nil sink reported emission")
	}
	return "", emitted
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
		preMsg, preEmitted := driveOneRequest(t, rt, "pre-mutation")

		// Immutability probe: mutate every field of the caller's config
		// and verify the runtime behaves identically.
		cfg.SamplingRate = flipRate(cfg.SamplingRate)
		cfg.Message = "mutated-message"
		if cfg.LevelSamplingRates != nil {
			for level := range cfg.LevelSamplingRates {
				delete(cfg.LevelSamplingRates, level)
			}
			cfg.LevelSamplingRates[LevelError] = 1
		}
		if cfg.OperationPolicies != nil {
			for domain, policy := range cfg.OperationPolicies {
				policy.SuccessLevel = LevelError
				policy.FailureLevel = LevelDebug
				if policy.OutcomeLevels != nil {
					policy.OutcomeLevels[OutcomeRetry] = LevelWarn
				}
				if policy.SamplingRate != nil {
					r := 0.0
					policy.SamplingRate = &r
				}
				cfg.OperationPolicies[domain] = policy
			}
		}
		postMsg, postEmitted := driveOneRequest(t, rt, "post-mutation")
		if postEmitted && preEmitted && postMsg != preMsg {
			t.Fatalf("runtime message changed after config mutation: %q -> %q",
				preMsg, postMsg)
		}
	})
}

// flipRate moves an accepted rate to a rejected one (or vice versa).
func flipRate(r float64) float64 {
	if r < 0.5 {
		return 1.5
	}
	return -1.5
}

// TestCompileConfigPropertyRandom drives the same oracle over
// PCG-generated configs — seed-only coverage without the fuzzer.
func TestCompileConfigPropertyRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xC0FFE5eed, 0x00F1E5))
	for i := 0; i < 2000; i++ {
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
		driveOneRequest(t, rt, fmt.Sprintf("iter-%d", i))
	}
}
