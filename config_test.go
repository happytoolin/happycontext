package hc

import "testing"

func TestNormalizeConfigClampsAndFiltersValues(t *testing.T) {
	rate := 2.5
	cfg := NormalizeConfig(Config{
		SamplingRate: 1.5,
		LevelSamplingRates: map[Level]float64{
			LevelDebug:       1.25,
			LevelWarn:        -0.5,
			Level("invalid"): 0.7,
		},
		OperationPolicies: map[Domain]OperationPolicy{
			"": {
				SuccessLevel: Level("invalid"),
				FailureLevel: LevelDebug,
				PanicLevel:   Level("invalid"),
				OutcomeLevels: map[Outcome]Level{
					OutcomeRetry:   LevelWarn,
					Outcome("bad"): LevelError,
					OutcomeFailure: Level("trace"),
				},
				SamplingRate: &rate,
			},
		},
	})

	if cfg.SamplingRate != 1 {
		t.Fatalf("SamplingRate = %v, want 1", cfg.SamplingRate)
	}
	if len(cfg.LevelSamplingRates) != 2 {
		t.Fatalf("expected 2 valid level sampling rates, got %d", len(cfg.LevelSamplingRates))
	}
	if cfg.LevelSamplingRates[LevelDebug] != 1 {
		t.Fatalf("LevelDebug sampling = %v, want 1", cfg.LevelSamplingRates[LevelDebug])
	}
	if cfg.LevelSamplingRates[LevelWarn] != 0 {
		t.Fatalf("LevelWarn sampling = %v, want 0", cfg.LevelSamplingRates[LevelWarn])
	}
	if _, ok := cfg.LevelSamplingRates[Level("invalid")]; ok {
		t.Fatal("did not expect invalid level sampling rate to survive normalization")
	}

	policy, ok := cfg.OperationPolicies[defaultDomainValue]
	if !ok {
		t.Fatalf("expected empty domain to normalize to %q", defaultDomainValue)
	}
	if policy.SuccessLevel != LevelInfo {
		t.Fatalf("SuccessLevel = %q, want %q", policy.SuccessLevel, LevelInfo)
	}
	if policy.FailureLevel != LevelDebug {
		t.Fatalf("FailureLevel = %q, want %q", policy.FailureLevel, LevelDebug)
	}
	if policy.PanicLevel != LevelError {
		t.Fatalf("PanicLevel = %q, want %q", policy.PanicLevel, LevelError)
	}
	if len(policy.OutcomeLevels) != 1 {
		t.Fatalf("expected only valid outcome-level override to remain, got %d entries", len(policy.OutcomeLevels))
	}
	if policy.OutcomeLevels[OutcomeRetry] != LevelWarn {
		t.Fatalf("OutcomeRetry level = %q, want %q", policy.OutcomeLevels[OutcomeRetry], LevelWarn)
	}
	if policy.SamplingRate == nil || *policy.SamplingRate != 1 {
		t.Fatalf("policy sampling rate = %v, want clamped pointer to 1", policy.SamplingRate)
	}
	if rate != 2.5 {
		t.Fatalf("NormalizeConfig should not mutate caller sampling pointer, rate = %v", rate)
	}
}

func TestNormalizeConfigCanonicalDomainOverridesAlias(t *testing.T) {
	aliasRate := 0.25
	canonicalRate := 0.75

	cfg := NormalizeConfig(Config{
		OperationPolicies: map[Domain]OperationPolicy{
			"": {
				SuccessLevel: LevelDebug,
				SamplingRate: &aliasRate,
			},
			defaultDomainValue: {
				SuccessLevel: LevelWarn,
				SamplingRate: &canonicalRate,
			},
		},
	})

	if len(cfg.OperationPolicies) != 1 {
		t.Fatalf("expected 1 canonical policy, got %d", len(cfg.OperationPolicies))
	}
	policy := cfg.OperationPolicies[defaultDomainValue]
	if policy.SuccessLevel != LevelWarn {
		t.Fatalf("SuccessLevel = %q, want %q", policy.SuccessLevel, LevelWarn)
	}
	if policy.SamplingRate == nil || *policy.SamplingRate != canonicalRate {
		t.Fatalf("SamplingRate = %v, want %v", policy.SamplingRate, canonicalRate)
	}
}
