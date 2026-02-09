package hlog

import (
	"time"
)

const defaultMessage = "request_completed"

// Config controls request finalization behavior.
type Config struct {
	// Sink receives the finalized event.
	Sink Sink

	// SamplingRate controls random sampling for non-error requests in [0,1].
	SamplingRate float64

	// Message is the final log message.
	Message string
}

// SampleInput is the sampling decision input.
type SampleInput struct {
	Method     string
	Path       string
	HasError   bool
	StatusCode int
	Duration   time.Duration
	Rate       float64
}
