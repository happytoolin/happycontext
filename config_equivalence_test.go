package hc

import (
	"math"
	"math/rand"
	"testing"
)

// referenceNormalizeConfig is the pre-fast-path implementation, kept verbatim
// as an oracle: NormalizeConfig must produce identical output for every input.
func referenceNormalizeConfig(cfg Config) Config {
	cfg.SamplingRate = clampRate(cfg.SamplingRate)

	if len(cfg.LevelSamplingRates) > 0 {
		clamped := make(map[Level]float64, len(cfg.LevelSamplingRates))
		for level, rate := range cfg.LevelSamplingRates {
			if !IsValidLevel(level) {
				continue
			}
			clamped[level] = clampRate(rate)
		}
		cfg.LevelSamplingRates = clamped
	}

	if len(cfg.OperationPolicies) > 0 {
		normalized := make(map[Domain]OperationPolicy, len(cfg.OperationPolicies))
		for domain, policy := range cfg.OperationPolicies {
			if domain == "" {
				continue
			}
			normalized[domain] = referenceNormalizeOperationPolicy(policy)
		}
		if aliasPolicy, ok := cfg.OperationPolicies[""]; ok {
			if _, exists := normalized[defaultDomainValue]; !exists {
				normalized[defaultDomainValue] = referenceNormalizeOperationPolicy(aliasPolicy)
			}
		}
		cfg.OperationPolicies = normalized
	}

	return cfg
}

func referenceNormalizeOperationPolicy(policy OperationPolicy) OperationPolicy {
	if !IsValidLevel(policy.SuccessLevel) {
		policy.SuccessLevel = LevelInfo
	}
	if !IsValidLevel(policy.FailureLevel) {
		policy.FailureLevel = LevelError
	}
	if !IsValidLevel(policy.PanicLevel) {
		policy.PanicLevel = LevelError
	}

	if len(policy.OutcomeLevels) > 0 {
		outcomeLevels := make(map[Outcome]Level, len(policy.OutcomeLevels))
		for outcome, level := range policy.OutcomeLevels {
			if !IsValidOutcome(outcome) || !IsValidLevel(level) {
				continue
			}
			outcomeLevels[outcome] = level
		}
		policy.OutcomeLevels = outcomeLevels
	}

	if policy.SamplingRate != nil {
		rate := clampRate(*policy.SamplingRate)
		policy.SamplingRate = &rate
	}

	return policy
}

func nastyConfigs() []Config {
	rateOutOfRange := 2.5
	rateNaN := math.NaN()
	rateNegInf := math.Inf(-1)
	ratePosInf := math.Inf(1)
	rateNegZero := math.Copysign(0, -1)
	rateFine := 0.5

	return []Config{
		{}, // zero config
		{SamplingRate: math.NaN()},
		{SamplingRate: math.Inf(1)},
		{SamplingRate: math.Inf(-1)},
		{SamplingRate: rateNegZero},
		{LevelSamplingRates: map[Level]float64{}}, // non-nil, empty
		{LevelSamplingRates: map[Level]float64{
			LevelDebug:       1.25,
			LevelWarn:        -0.5,
			Level("invalid"): 0.7,
			LevelError:       math.NaN(),
			LevelInfo:        math.Inf(1),
		}},
		{LevelSamplingRates: map[Level]float64{LevelInfo: 0.5}}, // already valid
		{OperationPolicies: map[Domain]OperationPolicy{}},       // non-nil, empty
		{OperationPolicies: map[Domain]OperationPolicy{
			"": {
				SuccessLevel: Level("invalid"),
				FailureLevel: LevelDebug,
				PanicLevel:   Level("invalid"),
				OutcomeLevels: map[Outcome]Level{
					OutcomeRetry:   LevelWarn,
					Outcome("bad"): LevelError,
					OutcomeFailure: Level("trace"),
				},
				SamplingRate: &rateOutOfRange,
			},
		}},
		{OperationPolicies: map[Domain]OperationPolicy{
			"api": {SuccessLevel: LevelWarn, FailureLevel: LevelError, PanicLevel: LevelError},
		}}, // already valid, no alias
		{OperationPolicies: map[Domain]OperationPolicy{
			"":    {SuccessLevel: LevelDebug, SamplingRate: &rateFine},
			"api": {SuccessLevel: LevelWarn, SamplingRate: &rateNaN},
		}},
		{OperationPolicies: map[Domain]OperationPolicy{
			"":                 {SuccessLevel: LevelDebug},
			defaultDomainValue: {SuccessLevel: LevelWarn}, // canonical must beat alias
		}},
		{OperationPolicies: map[Domain]OperationPolicy{
			"api": {
				SuccessLevel:  LevelDebug,
				FailureLevel:  LevelWarn,
				PanicLevel:    LevelError,
				OutcomeLevels: map[Outcome]Level{OutcomeRetry: LevelWarn},
				SamplingRate:  &rateNegInf,
			},
			"job": {OutcomeLevels: map[Outcome]Level{}}, // empty outcome map
		}},
		{OperationPolicies: map[Domain]OperationPolicy{
			"api": {SamplingRate: &ratePosInf},
		}},
		{OperationPolicies: map[Domain]OperationPolicy{
			"api": {
				SuccessLevel:  LevelInfo,
				FailureLevel:  LevelError,
				PanicLevel:    LevelError,
				OutcomeLevels: map[Outcome]Level{OutcomeRetry: LevelWarn},
				SamplingRate:  &rateNegZero, // fully valid policy
			},
		}},
	}
}

// configsEqual compares configs structurally. Unlike reflect.DeepEqual it
// tolerates NaN, and unlike %#v it dereferences sampling-rate pointers
// instead of comparing their addresses.
func configsEqual(a, b Config) bool {
	if !floatEqual(a.SamplingRate, b.SamplingRate) || a.Message != b.Message {
		return false
	}
	if (a.Sink == nil) != (b.Sink == nil) || (a.Sampler == nil) != (b.Sampler == nil) {
		return false
	}
	if (a.LevelSamplingRates == nil) != (b.LevelSamplingRates == nil) || len(a.LevelSamplingRates) != len(b.LevelSamplingRates) {
		return false
	}
	for level, ra := range a.LevelSamplingRates {
		rb, ok := b.LevelSamplingRates[level]
		if !ok || !floatEqual(ra, rb) {
			return false
		}
	}
	if (a.OperationPolicies == nil) != (b.OperationPolicies == nil) || len(a.OperationPolicies) != len(b.OperationPolicies) {
		return false
	}
	for domain, pa := range a.OperationPolicies {
		pb, ok := b.OperationPolicies[domain]
		if !ok || !policiesEqual(pa, pb) {
			return false
		}
	}
	return true
}

func policiesEqual(a, b OperationPolicy) bool {
	if a.SuccessLevel != b.SuccessLevel || a.FailureLevel != b.FailureLevel || a.PanicLevel != b.PanicLevel {
		return false
	}
	if (a.SamplingRate == nil) != (b.SamplingRate == nil) {
		return false
	}
	if a.SamplingRate != nil && !floatEqual(*a.SamplingRate, *b.SamplingRate) {
		return false
	}
	if (a.OutcomeLevels == nil) != (b.OutcomeLevels == nil) || len(a.OutcomeLevels) != len(b.OutcomeLevels) {
		return false
	}
	for outcome, la := range a.OutcomeLevels {
		lb, ok := b.OutcomeLevels[outcome]
		if !ok || la != lb {
			return false
		}
	}
	return true
}

func floatEqual(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || (math.IsNaN(a) && math.IsNaN(b))
}

func TestNormalizeConfigMatchesReference(t *testing.T) {
	for i, cfg := range nastyConfigs() {
		got := NormalizeConfig(cfg)
		want := referenceNormalizeConfig(cfg)
		if !configsEqual(got, want) {
			t.Fatalf("case %d: NormalizeConfig mismatch\ngot:  %#v\nwant: %#v", i, got, want)
		}
		if got2 := NormalizeConfig(NormalizeConfig(cfg)); !configsEqual(got2, got) {
			t.Fatalf("case %d: NormalizeConfig not idempotent\ngot:  %#v\nwant: %#v", i, got2, got)
		}
	}
}

func TestNormalizeConfigRandomizedMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	levels := []Level{"", LevelDebug, LevelInfo, LevelWarn, LevelError, Level("bogus")}
	outcomes := []Outcome{"", OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry, Outcome("weird")}
	rates := []float64{0, 1, -1, 2, 0.5, math.NaN(), math.Inf(1), math.Inf(-1), math.Copysign(0, -1), 1e-300}
	domains := []Domain{"", "http", "job", defaultDomainValue, Domain("")}

	randomLevel := func() Level { return levels[rng.Intn(len(levels))] }

	for i := 0; i < 2000; i++ {
		cfg := Config{SamplingRate: rates[rng.Intn(len(rates))]}

		if rng.Intn(2) == 0 {
			n := rng.Intn(4)
			m := make(map[Level]float64, n)
			for j := 0; j < n; j++ {
				m[randomLevel()] = rates[rng.Intn(len(rates))]
			}
			if rng.Intn(4) == 0 {
				m = nil
			}
			cfg.LevelSamplingRates = m
		}

		if rng.Intn(2) == 0 {
			n := rng.Intn(3)
			m := make(map[Domain]OperationPolicy, n)
			for j := 0; j < n; j++ {
				policy := OperationPolicy{
					SuccessLevel: randomLevel(),
					FailureLevel: randomLevel(),
					PanicLevel:   randomLevel(),
				}
				if rng.Intn(2) == 0 {
					n := rng.Intn(3)
					ol := make(map[Outcome]Level, n)
					for k := 0; k < n; k++ {
						ol[outcomes[rng.Intn(len(outcomes))]] = randomLevel()
					}
					policy.OutcomeLevels = ol
				}
				if rng.Intn(2) == 0 {
					r := rates[rng.Intn(len(rates))]
					policy.SamplingRate = &r
				}
				m[domains[rng.Intn(len(domains))]] = policy
			}
			if rng.Intn(4) == 0 {
				m = nil
			}
			cfg.OperationPolicies = m
		}

		got := NormalizeConfig(cfg)
		want := referenceNormalizeConfig(cfg)
		if !configsEqual(got, want) {
			t.Fatalf("iteration %d: mismatch\ncfg:  %#v\ngot:  %#v\nwant: %#v", i, cfg, got, want)
		}
	}
}

func TestNormalizeConfigIsolatesCallerMaps(t *testing.T) {
	rate := 0.5
	rates := map[Level]float64{LevelInfo: 0.5}
	policies := map[Domain]OperationPolicy{
		"api": {
			SuccessLevel:  LevelInfo,
			FailureLevel:  LevelError,
			PanicLevel:    LevelError,
			OutcomeLevels: map[Outcome]Level{OutcomeRetry: LevelWarn},
			SamplingRate:  &rate,
		},
	}

	cfg := NormalizeConfig(Config{LevelSamplingRates: rates, OperationPolicies: policies})

	// Caller mutates everything it still holds references to.
	rates[LevelInfo] = 0.9
	rates[LevelDebug] = 0.1
	mutated := policies["api"]
	mutated.FailureLevel = LevelDebug
	policies["api"] = mutated
	policies["extra"] = OperationPolicy{}
	policies["api"].OutcomeLevels[OutcomeRetry] = LevelError
	policies["api"].OutcomeLevels[Outcome("new")] = LevelDebug
	rate = 0.99

	if cfg.LevelSamplingRates[LevelInfo] != 0.5 {
		t.Fatalf("normalized level rate changed after caller mutation: %v", cfg.LevelSamplingRates[LevelInfo])
	}
	if _, ok := cfg.LevelSamplingRates[LevelDebug]; ok {
		t.Fatal("caller-added level rate leaked into normalized config")
	}
	if cfg.OperationPolicies["api"].FailureLevel != LevelError {
		t.Fatalf("normalized policy mutated via caller map: %v", cfg.OperationPolicies["api"].FailureLevel)
	}
	if _, ok := cfg.OperationPolicies["extra"]; ok {
		t.Fatal("caller-added policy leaked into normalized config")
	}
	if cfg.OperationPolicies["api"].OutcomeLevels[OutcomeRetry] != LevelWarn {
		t.Fatalf("normalized outcome level mutated via caller map: %v", cfg.OperationPolicies["api"].OutcomeLevels[OutcomeRetry])
	}
	if len(cfg.OperationPolicies["api"].OutcomeLevels) != 1 {
		t.Fatalf("caller-added outcome level leaked into normalized config: %d entries", len(cfg.OperationPolicies["api"].OutcomeLevels))
	}
	if cfg.OperationPolicies["api"].SamplingRate == nil || *cfg.OperationPolicies["api"].SamplingRate != 0.5 {
		t.Fatalf("normalized sampling rate changed via caller pointer: %v", cfg.OperationPolicies["api"].SamplingRate)
	}
}
