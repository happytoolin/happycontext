package common

import "github.com/happytoolin/happycontext"

// DefaultMessage is used when Config.Message is empty.
const DefaultMessage = hc.DefaultMessage

// NormalizeConfig clamps config values and applies defaults.
func NormalizeConfig(cfg hc.Config) hc.Config {
	cfg = hc.NormalizeConfig(cfg)
	if cfg.Message == "" {
		cfg.Message = DefaultMessage
	}
	return cfg
}

// MergeLevelWithFloor merges auto level with an optional requested level.
func MergeLevelWithFloor(autoLevel, requestedLevel hc.Level, hasRequested bool) hc.Level {
	return hc.MergeLevelWithFloor(autoLevel, requestedLevel, hasRequested)
}
