package common

import (
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestNormalizeConfigClampsAndDefaults(t *testing.T) {
	policyRate := 2.0
	tests := []struct {
		name    string
		cfg     hc.Config
		wantMsg string
		wantRat float64
	}{
		{name: "negative", cfg: hc.Config{SamplingRate: -1}, wantMsg: DefaultMessage, wantRat: 0},
		{name: "over one", cfg: hc.Config{SamplingRate: 2}, wantMsg: DefaultMessage, wantRat: 1},
		{name: "custom", cfg: hc.Config{SamplingRate: 0.5, Message: "done"}, wantMsg: "done", wantRat: 0.5},
		{
			name: "level rates",
			cfg: hc.Config{
				LevelSamplingRates: map[hc.Level]float64{
					hc.LevelDebug: 2,
					hc.LevelInfo:  -1,
					hc.Level("X"): 0.5,
				},
			},
			wantMsg: DefaultMessage,
			wantRat: 0,
		},
		{
			name: "operation policies",
			cfg: hc.Config{
				OperationPolicies: map[hc.Domain]hc.OperationPolicy{
					hc.DomainJob: {
						SuccessLevel: hc.Level("TRACE"),
						FailureLevel: hc.LevelWarn,
						PanicLevel:   hc.Level("PANIC"),
						OutcomeLevels: map[hc.Outcome]hc.Level{
							hc.OutcomeRetry: hc.LevelWarn,
							hc.Outcome("X"): hc.LevelError,
						},
						SamplingRate: &policyRate,
					},
				},
			},
			wantMsg: DefaultMessage,
			wantRat: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeConfig(tt.cfg)
			if got.Message != tt.wantMsg {
				t.Fatalf("message = %q, want %q", got.Message, tt.wantMsg)
			}
			if got.SamplingRate != tt.wantRat {
				t.Fatalf("sampling rate = %v, want %v", got.SamplingRate, tt.wantRat)
			}
			if tt.name == "level rates" {
				if got.LevelSamplingRates[hc.LevelDebug] != 1 {
					t.Fatalf("debug level rate = %v, want 1", got.LevelSamplingRates[hc.LevelDebug])
				}
				if got.LevelSamplingRates[hc.LevelInfo] != 0 {
					t.Fatalf("info level rate = %v, want 0", got.LevelSamplingRates[hc.LevelInfo])
				}
				if _, ok := got.LevelSamplingRates[hc.Level("X")]; ok {
					t.Fatal("expected invalid level to be removed")
				}
			}
			if tt.name == "operation policies" {
				pol := got.OperationPolicies[hc.DomainJob]
				if pol.SuccessLevel != hc.LevelInfo {
					t.Fatalf("success level = %s, want INFO", pol.SuccessLevel)
				}
				if pol.FailureLevel != hc.LevelWarn {
					t.Fatalf("failure level = %s, want WARN", pol.FailureLevel)
				}
				if pol.PanicLevel != hc.LevelError {
					t.Fatalf("panic level = %s, want ERROR", pol.PanicLevel)
				}
				if pol.OutcomeLevels[hc.OutcomeRetry] != hc.LevelWarn {
					t.Fatalf("retry level = %s, want WARN", pol.OutcomeLevels[hc.OutcomeRetry])
				}
				if _, ok := pol.OutcomeLevels[hc.Outcome("X")]; ok {
					t.Fatal("expected invalid outcome mapping to be removed")
				}
				if pol.SamplingRate == nil || *pol.SamplingRate != 1 {
					t.Fatalf("policy sampling = %v, want 1", pol.SamplingRate)
				}
			}
		})
	}
}

func TestMergeLevelWithFloor(t *testing.T) {
	tests := []struct {
		name         string
		auto         hc.Level
		requested    hc.Level
		hasRequested bool
		want         hc.Level
	}{
		{name: "no request", auto: hc.LevelInfo, want: hc.LevelInfo},
		{name: "invalid request", auto: hc.LevelInfo, requested: hc.Level("TRACE"), hasRequested: true, want: hc.LevelInfo},
		{name: "raise level", auto: hc.LevelInfo, requested: hc.LevelWarn, hasRequested: true, want: hc.LevelWarn},
		{name: "keep floor", auto: hc.LevelError, requested: hc.LevelDebug, hasRequested: true, want: hc.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeLevelWithFloor(tt.auto, tt.requested, tt.hasRequested)
			if got != tt.want {
				t.Fatalf("level = %q, want %q", got, tt.want)
			}
		})
	}
}
