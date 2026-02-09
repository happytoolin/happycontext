package common

import "github.com/happytoolin/hlog"

// DefaultMessage is used when Config.Message is empty.
const DefaultMessage = "request_completed"

// NormalizeConfig clamps config values and applies defaults.
func NormalizeConfig(cfg hlog.Config) hlog.Config {
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
	case hlog.LevelDebug:
		return 10
	case hlog.LevelInfo:
		return 20
	case hlog.LevelWarn:
		return 30
	case hlog.LevelError:
		return 40
	default:
		return 20
	}
}

func isValidLevel(level string) bool {
	switch level {
	case hlog.LevelDebug, hlog.LevelInfo, hlog.LevelWarn, hlog.LevelError:
		return true
	default:
		return false
	}
}
