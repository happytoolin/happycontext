package hc

func shouldWriteOperation(cfg Config, policy OperationPolicy, in SampleInput) bool {
	if cfg.Sampler != nil {
		return cfg.Sampler(in)
	}

	if in.HasError || in.StatusCode >= 500 {
		return true
	}

	rate := clampRate(cfg.SamplingRate)
	if policy.SamplingRate != nil {
		rate = clampRate(*policy.SamplingRate)
	} else if levelRate, ok := levelSamplingRate(cfg.LevelSamplingRates, in.Level); ok {
		rate = levelRate
	}
	return shouldSample(rate)
}

func policyForDomain(cfg Config, domain Domain) OperationPolicy {
	if cfg.OperationPolicies == nil {
		return OperationPolicy{}
	}
	domain = normalizeDomain(domain)
	if policy, ok := cfg.OperationPolicies[domain]; ok {
		return policy
	}
	if domain == defaultDomainValue {
		return cfg.OperationPolicies[""]
	}
	return OperationPolicy{}
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
