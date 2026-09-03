package hc

func levelFromPolicy(policy OperationPolicy, outcome Outcome) Level {
	if outcomeLevel, ok := policy.OutcomeLevels[outcome]; ok && IsValidLevel(outcomeLevel) {
		return outcomeLevel
	}

	// Zero policy fields mean the defaults; the zero Level is Info, which
	// is the success default anyway. Explicit per-outcome control beyond
	// that goes through OutcomeLevels.
	successLevel, failureLevel, panicLevel := LevelInfo, LevelError, LevelError
	if policy.SuccessLevel != 0 && IsValidLevel(policy.SuccessLevel) {
		successLevel = policy.SuccessLevel
	}
	if policy.FailureLevel != 0 && IsValidLevel(policy.FailureLevel) {
		failureLevel = policy.FailureLevel
	}
	if policy.PanicLevel != 0 && IsValidLevel(policy.PanicLevel) {
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
