package common

import "github.com/happytoolin/happycontext"

// DefaultMessage is used when Config.Message is empty.
const DefaultMessage = hc.DefaultMessage

// PreparedRequestConfig stores normalized request lifecycle config.
type PreparedRequestConfig struct {
	Config hc.Config
}

// NormalizeConfig clamps config values and applies defaults.
func NormalizeConfig(cfg hc.Config) hc.Config {
	cfg = hc.NormalizeConfig(cfg)
	if cfg.Message == "" {
		cfg.Message = DefaultMessage
	}
	return cfg
}

// PrepareRequestConfig normalizes request config once for middleware hot paths.
func PrepareRequestConfig(cfg hc.Config) PreparedRequestConfig {
	cfg = NormalizeConfig(cfg)
	return PreparedRequestConfig{Config: cfg}
}
