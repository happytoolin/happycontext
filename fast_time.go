package hc

import (
	"time"
	_ "unsafe"
)

//go:linkname runtimeNano runtime.nanotime
func runtimeNano() int64

func monotonicNow() int64 {
	return runtimeNano()
}

func durationSinceState(state eventState, startMono int64) time.Duration {
	if startMono != 0 {
		return time.Duration(monotonicNow() - startMono)
	}
	if state.startMono != 0 {
		return time.Duration(monotonicNow() - state.startMono)
	}
	return time.Since(state.startTime)
}
