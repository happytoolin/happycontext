package hc

import "testing"

func TestPolicyForDomainNormalizesEmptyDomain(t *testing.T) {
	cfg := Config{
		OperationPolicies: map[Domain]OperationPolicy{
			defaultDomainValue: {SuccessLevel: LevelDebug},
		},
	}

	policy := policyForDomain(cfg, "")
	if policy.SuccessLevel != LevelDebug {
		t.Fatalf("SuccessLevel = %q, want %q", policy.SuccessLevel, LevelDebug)
	}
}

func TestLevelFromPolicyPrefersOutcomeOverride(t *testing.T) {
	level := levelFromPolicy(OperationPolicy{
		SuccessLevel: LevelDebug,
		OutcomeLevels: map[Outcome]Level{
			OutcomeSuccess: LevelWarn,
		},
	}, OutcomeSuccess)

	if level != LevelWarn {
		t.Fatalf("level = %q, want %q", level, LevelWarn)
	}
}

func TestShouldWriteOperationSamplerOverridesAutomaticKeep(t *testing.T) {
	kept := shouldWriteOperation(Config{
		Sampler: func(SampleInput) bool { return false },
	}, OperationPolicy{}, SampleInput{
		HasError: true,
		Outcome:  OutcomeFailure,
	})

	if kept {
		t.Fatal("expected explicit sampler to override automatic keep behavior")
	}
}

func TestShouldWriteOperationKeepsHealthyOutcomeWhenPolicySamplingAllows(t *testing.T) {
	rate := 1.0
	kept := shouldWriteOperation(Config{
		SamplingRate: 0,
	}, OperationPolicy{
		SamplingRate: &rate,
	}, SampleInput{
		Outcome: OutcomeSuccess,
		Level:   LevelInfo,
	})

	if !kept {
		t.Fatal("expected policy sampling rate to keep healthy operation")
	}
}
