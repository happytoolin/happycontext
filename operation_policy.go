package hc

func shouldWriteOperation(cfg Config, policy OperationPolicy, in SampleInput) bool {
	if cfg.Sampler != nil {
		return cfg.Sampler(in)
	}

	return shouldWriteOperationDefault(cfg, policy, in.HasError, in.Code, in.StatusCode, in.Outcome, in.Level)
}

func shouldWriteOperationDefault(cfg Config, policy OperationPolicy, hasError bool, code, statusCode int, outcome Outcome, level Level) bool {
	if hasError || code >= 500 || statusCode >= 500 || outcome != OutcomeSuccess {
		return true
	}

	rate := clampRate(cfg.SamplingRate)
	if policy.SamplingRate != nil {
		rate = clampRate(*policy.SamplingRate)
	} else if levelRate, ok := levelSamplingRate(cfg.LevelSamplingRates, level); ok {
		rate = levelRate
	}
	return shouldSample(rate)
}

func shouldWriteOperationDefaultPrepared(prepared PreparedConfig, policy OperationPolicy, hasError bool, code, statusCode int, outcome Outcome, level Level) bool {
	if hasError || code >= 500 || statusCode >= 500 || outcome != OutcomeSuccess {
		return true
	}

	rate := prepared.cfg.SamplingRate
	if policy.SamplingRate != nil {
		rate = *policy.SamplingRate
	} else if prepared.hasLevelSamplingRate {
		if levelRate, ok := prepared.cfg.LevelSamplingRates[level]; ok {
			rate = levelRate
		}
	}
	return shouldSample(rate)
}

func policyForDomain(cfg Config, domain Domain) OperationPolicy {
	policy, _ := policyForDomainWithPresence(cfg, domain)
	return policy
}

func policyForDomainWithPresence(cfg Config, domain Domain) (OperationPolicy, bool) {
	if cfg.OperationPolicies == nil {
		return OperationPolicy{}, false
	}
	policy, ok := cfg.OperationPolicies[normalizeDomain(domain)]
	if !ok {
		return OperationPolicy{}, false
	}
	return policy, true
}

func defaultPolicy() OperationPolicy {
	return OperationPolicy{
		SuccessLevel: LevelInfo,
		FailureLevel: LevelError,
		PanicLevel:   LevelError,
	}
}

func levelFromPolicy(policy OperationPolicy, outcome Outcome) Level {
	def := defaultPolicy()
	if outcomeLevel, ok := policy.OutcomeLevels[outcome]; ok && IsValidLevel(outcomeLevel) {
		return outcomeLevel
	}

	successLevel := def.SuccessLevel
	if IsValidLevel(policy.SuccessLevel) {
		successLevel = policy.SuccessLevel
	}
	failureLevel := def.FailureLevel
	if IsValidLevel(policy.FailureLevel) {
		failureLevel = policy.FailureLevel
	}
	panicLevel := def.PanicLevel
	if IsValidLevel(policy.PanicLevel) {
		panicLevel = policy.PanicLevel
	}

	switch outcome {
	case OutcomeSuccess:
		return successLevel
	case OutcomePanic:
		return panicLevel
	default:
		return failureLevel
	}
}

func defaultLevelForOutcome(outcome Outcome) Level {
	if outcome == OutcomeSuccess {
		return LevelInfo
	}
	return LevelError
}

func clampRate(rate float64) float64 {
	if rate < 0 {
		return 0
	}
	if rate > 1 {
		return 1
	}
	return rate
}

func levelSamplingRate(rates map[Level]float64, level Level) (float64, bool) {
	if rates == nil {
		return 0, false
	}
	rate, ok := rates[level]
	if !ok {
		return 0, false
	}
	return clampRate(rate), true
}
