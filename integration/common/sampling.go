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

func shouldWriteEvent(in hc.SampleInput) bool {
	if in.HasError || in.StatusCode >= 500 {
		return true
	}
	return shouldSample(in.Rate)
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
