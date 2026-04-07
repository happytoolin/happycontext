package hc

import "testing"

func TestIsValidLevel(t *testing.T) {
	tests := []struct {
		level    Level
		expected bool
	}{
		{LevelDebug, true},
		{LevelInfo, true},
		{LevelWarn, true},
		{LevelError, true},
		{Level("invalid"), false},
		{Level(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got := IsValidLevel(tt.level)
			if got != tt.expected {
				t.Errorf("IsValidLevel(%q) = %v, want %v", tt.level, got, tt.expected)
			}
		})
	}
}

func TestLevelRank(t *testing.T) {
	tests := []struct {
		level    Level
		expected int
	}{
		{LevelDebug, 10},
		{LevelInfo, 20},
		{LevelWarn, 30},
		{LevelError, 40},
		{Level("unknown"), 20}, // defaults to Info rank
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got := LevelRank(tt.level)
			if got != tt.expected {
				t.Errorf("LevelRank(%q) = %d, want %d", tt.level, got, tt.expected)
			}
		})
	}
}

func TestIsValidOutcome(t *testing.T) {
	tests := []struct {
		outcome  Outcome
		expected bool
	}{
		{OutcomeSuccess, true},
		{OutcomeFailure, true},
		{OutcomePanic, true},
		{OutcomeCanceled, true},
		{OutcomeTimeout, true},
		{OutcomeRetry, true},
		{Outcome("invalid"), false},
		{Outcome(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.outcome), func(t *testing.T) {
			got := IsValidOutcome(tt.outcome)
			if got != tt.expected {
				t.Errorf("IsValidOutcome(%q) = %v, want %v", tt.outcome, got, tt.expected)
			}
		})
	}
}

func TestMergeLevelWithFloor(t *testing.T) {
	tests := []struct {
		name         string
		auto         Level
		requested    Level
		hasRequested bool
		want         Level
	}{
		{name: "no requested floor", auto: LevelInfo, requested: LevelWarn, hasRequested: false, want: LevelInfo},
		{name: "invalid requested floor ignored", auto: LevelInfo, requested: Level("trace"), hasRequested: true, want: LevelInfo},
		{name: "higher requested floor wins", auto: LevelInfo, requested: LevelWarn, hasRequested: true, want: LevelWarn},
		{name: "lower requested floor does not downgrade", auto: LevelError, requested: LevelInfo, hasRequested: true, want: LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeLevelWithFloor(tt.auto, tt.requested, tt.hasRequested)
			if got != tt.want {
				t.Fatalf("MergeLevelWithFloor(%q, %q, %v) = %q, want %q", tt.auto, tt.requested, tt.hasRequested, got, tt.want)
			}
		})
	}
}
