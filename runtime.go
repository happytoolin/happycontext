package hc

import (
	"context"
	"errors"
	"fmt"
	"maps"
)

// Compile-time error contract (amendment 17).
var (
	// ErrInvalidRate wraps out-of-range sampling rates.
	ErrInvalidRate = errors.New("hc: invalid rate")
	// ErrInvalidLevel wraps unknown level keys in configuration maps.
	ErrInvalidLevel = errors.New("hc: invalid level")
	// ErrInvalidOutcome wraps unknown outcome keys in configuration maps.
	ErrInvalidOutcome = errors.New("hc: invalid outcome")
)

// Config describes how a Runtime finalizes events. Bad configuration is
// a construction-time error (Compile), never a per-request clamp.
type Config struct {
	// Sink receives finalized records. Nil is valid: the runtime emits
	// nothing (the v0 zero-config behavior, kept deliberately).
	Sink Sink

	// SamplingRate controls random sampling for healthy traffic in [0,1].
	SamplingRate float64

	// LevelSamplingRates optionally overrides SamplingRate by final
	// level. Keys must be valid levels; values in [0,1].
	LevelSamplingRates map[Level]float64

	// Sampler overrides built-in rate sampling when set. Errors and
	// panics bypass it structurally (amendment 4).
	Sampler Sampler

	// OperationPolicies optionally customizes lifecycle behavior by
	// domain; a domain SamplingRate overrides generic rates.
	OperationPolicies map[Domain]OperationPolicy

	// Message is the default final message, overriding the per-domain
	// defaults (request_completed / operation_completed).
	Message string
}

// Runtime is the immutable, compiled form of Config, shared by all
// requests. Integrations take *Runtime, never Config.
type Runtime struct {
	sink       Sink
	sampler    Sampler
	rate       float64
	levelRates map[Level]float64
	policies   map[Domain]OperationPolicy
	message    string
}

// Compile validates cfg and returns an immutable *Runtime. Errors wrap
// the sentinel values (ErrInvalidRate, ErrInvalidLevel, ErrInvalidOutcome)
// with %w and an "hc: " prefix; errors.Is works.
func Compile(cfg Config) (*Runtime, error) {
	rt := &Runtime{
		sink:    cfg.Sink,
		sampler: cfg.Sampler,
		message: cfg.Message,
		rate:    cfg.SamplingRate,
	}

	if !(rt.rate >= 0 && rt.rate <= 1) {
		return nil, fmt.Errorf("hc: sampling rate %g: %w", rt.rate, ErrInvalidRate)
	}

	if len(cfg.LevelSamplingRates) > 0 {
		rt.levelRates = make(map[Level]float64, len(cfg.LevelSamplingRates))
		for level, rate := range cfg.LevelSamplingRates {
			if !IsValidLevel(level) {
				return nil, fmt.Errorf("hc: level sampling rate for level %d: %w", int(level), ErrInvalidLevel)
			}
			if !(rate >= 0 && rate <= 1) {
				return nil, fmt.Errorf("hc: sampling rate %g for %s: %w", rate, level, ErrInvalidRate)
			}
			rt.levelRates[level] = rate
		}
	}

	if len(cfg.OperationPolicies) > 0 {
		rt.policies = make(map[Domain]OperationPolicy, len(cfg.OperationPolicies))
		alias, hasAlias := cfg.OperationPolicies[""]
		if hasAlias {
			if err := validatePolicy(alias); err != nil {
				return nil, fmt.Errorf("hc: policy for domain %q: %w", defaultDomainValue, err)
			}
		}
		for domain, policy := range cfg.OperationPolicies {
			explicit := domain != ""
			if !explicit {
				domain = defaultDomainValue
			}
			if _, clash := rt.policies[domain]; clash && !explicit {
				continue // an explicit key wins over the "" alias deterministically
			}
			if err := validatePolicy(policy); err != nil {
				return nil, fmt.Errorf("hc: policy for domain %q: %w", domain, err)
			}
			rt.policies[domain] = copyPolicy(policy)
		}
	}

	return rt, nil
}

// MustCompile is Compile for literal configurations in main; a bad
// config panics at startup, the regexp idiom.
func MustCompile(cfg Config) *Runtime {
	rt, err := Compile(cfg)
	if err != nil {
		panic(err)
	}
	return rt
}

func validatePolicy(policy OperationPolicy) error {
	if !IsValidLevel(policy.SuccessLevel) {
		return fmt.Errorf("success level %d: %w", int(policy.SuccessLevel), ErrInvalidLevel)
	}
	if !IsValidLevel(policy.FailureLevel) {
		return fmt.Errorf("failure level %d: %w", int(policy.FailureLevel), ErrInvalidLevel)
	}
	if !IsValidLevel(policy.PanicLevel) {
		return fmt.Errorf("panic level %d: %w", int(policy.PanicLevel), ErrInvalidLevel)
	}
	for outcome, level := range policy.OutcomeLevels {
		if !IsValidOutcome(outcome) {
			return fmt.Errorf("outcome %q: %w", string(outcome), ErrInvalidOutcome)
		}
		if !IsValidLevel(level) {
			return fmt.Errorf("level %d for outcome %q: %w", int(level), string(outcome), ErrInvalidLevel)
		}
	}
	if policy.SamplingRate != nil {
		if r := *policy.SamplingRate; !(r >= 0 && r <= 1) {
			return fmt.Errorf("sampling rate %g: %w", r, ErrInvalidRate)
		}
	}
	return nil
}

// copyPolicy deep-copies a policy so the compiled runtime shares no
// mutable state with the caller's Config.
func copyPolicy(policy OperationPolicy) OperationPolicy {
	if policy.OutcomeLevels != nil {
		policy.OutcomeLevels = maps.Clone(policy.OutcomeLevels)
	}
	if policy.SamplingRate != nil {
		rate := *policy.SamplingRate
		policy.SamplingRate = &rate
	}
	return policy
}

// noop reports whether the runtime can never emit (nil sink).
func (rt *Runtime) noop() bool { return rt == nil || rt.sink == nil }

// policyFor returns the domain policy (zero value when unconfigured).
func (rt *Runtime) policyFor(domain Domain) OperationPolicy {
	if rt.policies == nil {
		return OperationPolicy{}
	}
	domain = normalizeDomain(domain)
	if policy, ok := rt.policies[domain]; ok {
		return policy
	}
	return OperationPolicy{}
}

// Emit writes rec to the configured sink. The nil-runtime and nil-sink
// cases are no-ops by contract.
func (rt *Runtime) emit(ctx context.Context, rec *Record) {
	if rt.noop() {
		return
	}
	rt.sink.Write(ctx, rec)
}
