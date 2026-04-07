package hc

// NormalizeConfig clamps config values and normalizes policy maps.
func NormalizeConfig(cfg Config) Config {
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
			d := domain
			if d == "" {
				d = defaultDomainValue
			}

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

			normalized[d] = policy
		}
		cfg.OperationPolicies = normalized
	}

	return cfg
}
