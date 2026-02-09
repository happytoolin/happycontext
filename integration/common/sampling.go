package common

import (
	"sync/atomic"
	"time"

	"github.com/happytoolin/happycontext"
)

var samplerState atomic.Uint64

func init() {
	samplerState.Store(uint64(time.Now().UnixNano()) + 0x9e3779b97f4a7c15)
}

type sampleInput struct {
	Method     string
	Path       string
	HasError   bool
	StatusCode int
	Duration   time.Duration
	Level      hc.Level
	Rate       float64
	Event      *hc.Event
}

func shouldWriteEvent(cfg hc.Config, in sampleInput) bool {
	if cfg.Sampler != nil {
		return cfg.Sampler(hc.SampleInput{
			Method:     in.Method,
			Path:       in.Path,
			StatusCode: in.StatusCode,
			Duration:   in.Duration,
			Level:      in.Level,
			HasError:   in.HasError,
			Event:      in.Event,
		})
	}

	if in.HasError || in.StatusCode >= 500 {
		return true
	}

	rate := in.Rate
	if cfg.LevelSamplingRates != nil {
		if levelRate, ok := cfg.LevelSamplingRates[in.Level]; ok {
			rate = levelRate
		}
	}
	return shouldSample(rate)
}

func shouldSample(rate float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	return nextSampleFloat64() < rate
}

func nextSampleFloat64() float64 {
	x := samplerState.Add(0x9e3779b97f4a7c15)
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	x *= 2685821657736338717
	return float64(x>>11) * (1.0 / (1 << 53))
}
