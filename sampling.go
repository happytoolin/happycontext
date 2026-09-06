package hc

import (
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// SampleInput contains finalized operation data used for sampling
// decisions: the resolved scalars plus read access to the request's
// fields (Lookup and a zero-copy Fields view).
type SampleInput struct {
	Domain    Domain
	Operation string
	Outcome   Outcome
	Code      int

	// HTTP compatibility fields. For non-HTTP operations, these may be empty/zero.
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	Level      Level
	HasError   bool

	// ev backs Lookup/Fields; nil when the input was built synthetically.
	ev *event
}

// Lookup returns the last value written under key on the request's WAL.
func (in SampleInput) Lookup(key string) (any, bool) {
	if in.ev == nil {
		return nil, false
	}
	return in.ev.lookup(key)
}

// Fields returns a read-only view of the request's fields in insertion
// order (amendment 8).
func (in SampleInput) Fields() []Field {
	if in.ev == nil {
		return nil
	}
	return in.ev.fields
}

// Sampler returns true when an event should be written.
type Sampler func(SampleInput) bool

// SamplerMiddleware wraps a sampler with additional decision logic.
type SamplerMiddleware func(next Sampler) Sampler

// ChainSampler composes base with middlewares.
//
// Middlewares are applied in declaration order:
// ChainSampler(base, a, b) == a(b(base)).
func ChainSampler(base Sampler, middlewares ...SamplerMiddleware) Sampler {
	if base == nil {
		base = NeverSampler()
	}
	chained := base
	for _, middleware := range slices.Backward(middlewares) {
		if middleware == nil {
			continue
		}
		chained = middleware(chained)
	}
	return chained
}

// NeverSampler returns a sampler that drops every event.
func NeverSampler() Sampler {
	return func(SampleInput) bool { return false }
}

// AlwaysSampler returns a sampler that keeps every event.
func AlwaysSampler() Sampler {
	return func(SampleInput) bool { return true }
}

// KeepErrors returns middleware that keeps errored requests.
func KeepErrors() SamplerMiddleware {
	return func(next Sampler) Sampler {
		return func(in SampleInput) bool {
			return in.HasError || in.Code >= 500 || in.StatusCode >= 500 || next(in)
		}
	}
}

// KeepSlowerThan returns middleware that keeps requests at/above minDuration.
//
// Negative durations are treated as zero.
func KeepSlowerThan(minDuration time.Duration) SamplerMiddleware {
	minDuration = max(minDuration, 0)
	return func(next Sampler) Sampler {
		return func(in SampleInput) bool {
			return in.Duration >= minDuration || next(in)
		}
	}
}

// KeepPathPrefix returns middleware that keeps requests matching path prefixes.
func KeepPathPrefix(prefixes ...string) SamplerMiddleware {
	filtered := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return func(next Sampler) Sampler { return next }
	}
	return func(next Sampler) Sampler {
		return func(in SampleInput) bool {
			for _, prefix := range filtered {
				if strings.HasPrefix(in.Path, prefix) {
					return true
				}
			}
			return next(in)
		}
	}
}

// RateSampler returns a probabilistic sampler using rate in [0,1].
//
// NaN and values <= 0 always drop. Values >= 1 always keep.
func RateSampler(rate float64) Sampler {
	switch {
	case !(rate > 0):
		return NeverSampler()
	case rate >= 1:
		return AlwaysSampler()
	default:
		return func(SampleInput) bool {
			return shouldSample(rate)
		}
	}
}

func shouldSample(rate float64) bool {
	if !(rate > 0) {
		return false
	}
	if rate >= 1 {
		return true
	}
	return rand.Float64() < rate
}
