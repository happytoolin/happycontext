package hc

import (
	"strings"
	"sync/atomic"
	"time"
)

// SampleInput contains finalized request data used for sampling decisions.
type SampleInput struct {
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	Level      Level
	HasError   bool
	Event      *Event
}

// Sampler returns true when an event should be written.
type Sampler func(SampleInput) bool

// SamplerMiddleware wraps a sampler with additional decision logic.
type SamplerMiddleware func(next Sampler) Sampler

var samplerState atomic.Uint64

func init() {
	samplerState.Store(uint64(time.Now().UnixNano()) + 0x9e3779b97f4a7c15)
}

// ChainSampler composes base with middlewares.
//
// Middlewares are applied in declaration order:
// ChainSampler(base, a, b) == a(b(base)).
func ChainSampler(base Sampler, middlewares ...SamplerMiddleware) Sampler {
	if base == nil {
		base = NeverSampler()
	}
	chained := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] == nil {
			continue
		}
		chained = middlewares[i](chained)
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
			return in.HasError || in.StatusCode >= 500 || next(in)
		}
	}
}

// KeepSlowerThan returns middleware that keeps requests at/above minDuration.
//
// Negative durations are treated as zero.
func KeepSlowerThan(minDuration time.Duration) SamplerMiddleware {
	if minDuration < 0 {
		minDuration = 0
	}
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
// Values <= 0 always drop. Values >= 1 always keep.
func RateSampler(rate float64) Sampler {
	switch {
	case rate <= 0:
		return NeverSampler()
	case rate >= 1:
		return AlwaysSampler()
	default:
		return func(in SampleInput) bool {
			return nextSampleFloat64() < rate
		}
	}
}

func nextSampleFloat64() float64 {
	x := samplerState.Add(0x9e3779b97f4a7c15)
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	x *= 2685821657736338717
	return float64(x>>11) * (1.0 / (1 << 53))
}
