package common

import "github.com/happytoolin/happycontext"

// DefaultMessage is used when Config.Message is empty.
const DefaultMessage = "request_completed"

// NormalizeConfig clamps config values and applies defaults.
func NormalizeConfig(cfg hc.Config) hc.Config {
	if cfg.SamplingRate < 0 {
		cfg.SamplingRate = 0
	}
	if cfg.SamplingRate > 1 {
		cfg.SamplingRate = 1
	}
	if len(cfg.LevelSamplingRates) > 0 {
		clamped := make(map[hc.Level]float64, len(cfg.LevelSamplingRates))
		for level, rate := range cfg.LevelSamplingRates {
			if !hc.IsValidLevel(level) {
				continue
			}
			if rate < 0 {
				rate = 0
			}
			if rate > 1 {
				rate = 1
			}
			clamped[level] = rate
		}
		cfg.LevelSamplingRates = clamped
	}
	if len(cfg.OperationPolicies) > 0 {
		normalized := make(map[hc.Domain]hc.OperationPolicy, len(cfg.OperationPolicies))
		for domain, policy := range cfg.OperationPolicies {
			d := domain
			if d == "" {
				d = hc.Domain("operation")
			}

			if !hc.IsValidLevel(policy.SuccessLevel) {
				policy.SuccessLevel = hc.LevelInfo
			}
			if !hc.IsValidLevel(policy.FailureLevel) {
				policy.FailureLevel = hc.LevelError
			}
			if !hc.IsValidLevel(policy.PanicLevel) {
				policy.PanicLevel = hc.LevelError
			}

			if len(policy.OutcomeLevels) > 0 {
				outcomeLevels := make(map[hc.Outcome]hc.Level, len(policy.OutcomeLevels))
				for outcome, level := range policy.OutcomeLevels {
					if !hc.IsValidOutcome(outcome) || !hc.IsValidLevel(level) {
						continue
					}
					outcomeLevels[outcome] = level
				}
				policy.OutcomeLevels = outcomeLevels
			}

			if policy.SamplingRate != nil {
				rate := *policy.SamplingRate
				if rate < 0 {
					rate = 0
				}
				if rate > 1 {
					rate = 1
				}
				policy.SamplingRate = &rate
			}

			normalized[d] = policy
		}
		cfg.OperationPolicies = normalized
	}
	if cfg.Message == "" {
		cfg.Message = DefaultMessage
	}
	return cfg
}

// MergeLevelWithFloor merges auto level with an optional requested level.
func MergeLevelWithFloor(autoLevel, requestedLevel hc.Level, hasRequested bool) hc.Level {
	if !hasRequested || !hc.IsValidLevel(requestedLevel) {
		return autoLevel
	}
	if hc.LevelRank(requestedLevel) > hc.LevelRank(autoLevel) {
		return requestedLevel
	}
	return autoLevel
}
