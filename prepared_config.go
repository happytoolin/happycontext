package hc

// preparedConfig stores normalized config for repeated hot-path finalization.
//
// Build one with prepareConfig and reuse it when the same Config is used for
// many operations.
type preparedConfig struct {
	cfg                  Config
	hasLevelSamplingRate bool
	hasOperationPolicy   bool
	fastDefaultOperation bool
}

// prepareConfig normalizes cfg once for repeated use.
func prepareConfig(cfg Config) preparedConfig {
	cfg = NormalizeConfig(cfg)
	return preparedConfig{
		cfg:                  cfg,
		hasLevelSamplingRate: len(cfg.LevelSamplingRates) > 0,
		hasOperationPolicy:   len(cfg.OperationPolicies) > 0,
		fastDefaultOperation: cfg.Sampler == nil && len(cfg.Enrichers) == 0 && cfg.FieldMapper == nil && len(cfg.LevelSamplingRates) == 0 && len(cfg.OperationPolicies) == 0,
	}
}

// Config returns the normalized Config stored in prepared.
func (prepared preparedConfig) Config() Config {
	return prepared.cfg
}

func (prepared preparedConfig) policyForDomain(domain Domain) (OperationPolicy, bool) {
	if !prepared.hasOperationPolicy {
		return OperationPolicy{}, false
	}
	policy, ok := prepared.cfg.OperationPolicies[normalizeDomain(domain)]
	if !ok {
		return OperationPolicy{}, false
	}
	return policy, true
}
