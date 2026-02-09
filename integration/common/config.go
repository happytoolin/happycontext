package common

import "github.com/happytoolin/happycontext"

// DefaultMessage is used when Config.Message is empty.
const DefaultMessage = "request_completed"

// NormalizeConfig clamps config values and applies defaults.
func NormalizeConfig(cfg happycontext.Config) happycontext.Config {
	if cfg.SamplingRate < 0 {
		cfg.SamplingRate = 0
	}
	if cfg.SamplingRate > 1 {
		cfg.SamplingRate = 1
	}
	if cfg.Message == "" {
		cfg.Message = DefaultMessage
	}
	return cfg
}

// MergeLevelWithFloor merges auto level with an optional requested level.
func MergeLevelWithFloor(autoLevel, requestedLevel string, hasRequested bool) string {
	if !hasRequested || !isValidLevel(requestedLevel) {
		return autoLevel
	}
	if levelRank(requestedLevel) > levelRank(autoLevel) {
		return requestedLevel
	}
	return autoLevel
}

func levelRank(level string) int {
	switch level {
	case happycontext.LevelDebug:
		return 10
	case happycontext.LevelInfo:
		return 20
	case happycontext.LevelWarn:
		return 30
	case happycontext.LevelError:
		return 40
	default:
		return 20
	}
}

func isValidLevel(level string) bool {
	switch level {
	case happycontext.LevelDebug, happycontext.LevelInfo, happycontext.LevelWarn, happycontext.LevelError:
		return true
	default:
		return false
	}
}
