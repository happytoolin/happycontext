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
			if !isValidLevel(level) {
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

			if !isValidLevel(policy.SuccessLevel) {
				policy.SuccessLevel = hc.LevelInfo
			}
			if !isValidLevel(policy.FailureLevel) {
				policy.FailureLevel = hc.LevelError
			}
			if !isValidLevel(policy.PanicLevel) {
				policy.PanicLevel = hc.LevelError
			}

			if len(policy.OutcomeLevels) > 0 {
				outcomeLevels := make(map[hc.Outcome]hc.Level, len(policy.OutcomeLevels))
				for outcome, level := range policy.OutcomeLevels {
					if !isValidOutcome(outcome) || !isValidLevel(level) {
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
	if !hasRequested || !isValidLevel(requestedLevel) {
		return autoLevel
	}
	if levelRank(requestedLevel) > levelRank(autoLevel) {
		return requestedLevel
	}
	return autoLevel
}

func levelRank(level hc.Level) int {
	switch level {
	case hc.LevelDebug:
		return 10
	case hc.LevelInfo:
		return 20
	case hc.LevelWarn:
		return 30
	case hc.LevelError:
		return 40
	default:
		return 20
	}
}

func isValidLevel(level hc.Level) bool {
	switch level {
	case hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError:
		return true
	default:
		return false
	}
}

func isValidOutcome(outcome hc.Outcome) bool {
	switch outcome {
	case hc.OutcomeSuccess, hc.OutcomeFailure, hc.OutcomePanic, hc.OutcomeCanceled, hc.OutcomeTimeout, hc.OutcomeRetry:
		return true
	default:
		return false
	}
}
