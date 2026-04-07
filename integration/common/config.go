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
