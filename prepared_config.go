package hc

// PreparedConfig stores normalized config for repeated hot-path finalization.
//
// Build one with PrepareConfig and reuse it when the same Config is used for
// many operations.
type PreparedConfig struct {
	cfg                  Config
	hasLevelSamplingRate bool
	hasOperationPolicy   bool
	fastDefaultOperation bool
}

// PrepareConfig normalizes cfg once for repeated use.
func PrepareConfig(cfg Config) PreparedConfig {
	cfg = NormalizeConfig(cfg)
	return PreparedConfig{
		cfg:                  cfg,
		hasLevelSamplingRate: len(cfg.LevelSamplingRates) > 0,
		hasOperationPolicy:   len(cfg.OperationPolicies) > 0,
		fastDefaultOperation: cfg.Sampler == nil && len(cfg.Enrichers) == 0 && cfg.FieldMapper == nil && len(cfg.LevelSamplingRates) == 0 && len(cfg.OperationPolicies) == 0,
	}
}

// Config returns the normalized Config stored in prepared.
func (prepared PreparedConfig) Config() Config {
	return prepared.cfg
}

func (prepared PreparedConfig) policyForDomain(domain Domain) (OperationPolicy, bool) {
	if !prepared.hasOperationPolicy {
		return OperationPolicy{}, false
	}
	policy, ok := prepared.cfg.OperationPolicies[normalizeDomain(domain)]
	if !ok {
		return OperationPolicy{}, false
	}
	return policy, true
}
