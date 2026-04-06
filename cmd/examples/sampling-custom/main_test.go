package main

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/adapter/slog"
)

func TestSamplingCustomSampler(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	sink := slogadapter.New(logger)

	// Create a custom sampler that keeps errors and slow requests
	customSampler := func(in hc.SampleInput) bool {
		// Keep all errors
		if in.HasError || in.Code >= 500 {
			return true
		}
		// Keep slow requests (>= 100ms)
		if in.Duration >= 100*time.Millisecond {
			return true
		}
		// Sample 10% of remaining
		return in.Code%10 == 0
	}

	cfg := hc.Config{
		Sink:    sink,
		Sampler: customSampler,
		Message: "custom sampling test",
	}

	// Test with various inputs
	testCases := []struct {
		name     string
		input    hc.SampleInput
		expected bool
	}{
		{
			name: "error should be sampled",
			input: hc.SampleInput{
				Domain:   hc.DomainHTTP,
				Outcome:  hc.OutcomeFailure,
				Code:     200,
				HasError: true,
			},
			expected: true,
		},
		{
			name: "5xx error should be sampled",
			input: hc.SampleInput{
				Domain:  hc.DomainHTTP,
				Outcome: hc.OutcomeFailure,
				Code:    500,
			},
			expected: true,
		},
		{
			name: "slow request should be sampled",
			input: hc.SampleInput{
				Domain:   hc.DomainHTTP,
				Outcome:  hc.OutcomeSuccess,
				Code:     200,
				Duration: 150 * time.Millisecond,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := customSampler(tc.input)
			if result != tc.expected {
				t.Errorf("customSampler() = %v, want %v", result, tc.expected)
			}
		})
	}

	_ = cfg
}
