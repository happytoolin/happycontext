package hc

import "maps"

// NormalizeConfig clamps config values and normalizes policy maps.
//
// The returned config shares no mutable state with cfg: mutating cfg or its
// maps afterwards never affects the normalized result.
func NormalizeConfig(cfg Config) Config {
	cfg = normalizeConfigShared(cfg)

	if cfg.LevelSamplingRates != nil {
		cfg.LevelSamplingRates = maps.Clone(cfg.LevelSamplingRates)
	}
	if cfg.OperationPolicies != nil {
		policies := make(map[Domain]OperationPolicy, len(cfg.OperationPolicies))
		for domain, policy := range cfg.OperationPolicies {
			if policy.OutcomeLevels != nil {
				policy.OutcomeLevels = maps.Clone(policy.OutcomeLevels)
			}
			if policy.SamplingRate != nil {
				rate := *policy.SamplingRate
				policy.SamplingRate = &rate
			}
			policies[domain] = policy
		}
		cfg.OperationPolicies = policies
	}
	return cfg
}

// normalizeConfigShared normalizes like NormalizeConfig but may share maps
// with cfg when they are already normalized. It is safe for internal
// per-request use, where the config is trusted to be read-only.
func normalizeConfigShared(cfg Config) Config {
	cfg.SamplingRate = clampRate(cfg.SamplingRate)

	if len(cfg.LevelSamplingRates) > 0 && levelRatesNeedNormalization(cfg.LevelSamplingRates) {
		clamped := make(map[Level]float64, len(cfg.LevelSamplingRates))
		for level, rate := range cfg.LevelSamplingRates {
			if !IsValidLevel(level) {
				continue
			}
			clamped[level] = clampRate(rate)
		}
		cfg.LevelSamplingRates = clamped
	}

	if len(cfg.OperationPolicies) > 0 && operationPoliciesNeedNormalization(cfg.OperationPolicies) {
		normalized := make(map[Domain]OperationPolicy, len(cfg.OperationPolicies))
		for domain, policy := range cfg.OperationPolicies {
			if domain == "" {
				continue
			}
			normalized[domain] = normalizeOperationPolicy(policy)
		}
		if aliasPolicy, ok := cfg.OperationPolicies[""]; ok {
			if _, exists := normalized[defaultDomainValue]; !exists {
				normalized[defaultDomainValue] = normalizeOperationPolicy(aliasPolicy)
			}
		}
		cfg.OperationPolicies = normalized
	}

	return cfg
}

func levelRatesNeedNormalization(rates map[Level]float64) bool {
	for level, rate := range rates {
		if !IsValidLevel(level) || rate < 0 || rate > 1 {
			return true
		}
	}
	return false
}

func operationPoliciesNeedNormalization(policies map[Domain]OperationPolicy) bool {
	for domain, policy := range policies {
		if domain == "" {
			return true
		}
		if operationPolicyNeedsNormalization(policy) {
			return true
		}
	}
	return false
}

func operationPolicyNeedsNormalization(policy OperationPolicy) bool {
	if !IsValidLevel(policy.SuccessLevel) || !IsValidLevel(policy.FailureLevel) || !IsValidLevel(policy.PanicLevel) {
		return true
	}
	for outcome, level := range policy.OutcomeLevels {
		if !IsValidOutcome(outcome) || !IsValidLevel(level) {
			return true
		}
	}
	if policy.SamplingRate != nil && (*policy.SamplingRate < 0 || *policy.SamplingRate > 1) {
		return true
	}
	return false
}

func normalizeOperationPolicy(policy OperationPolicy) OperationPolicy {
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
