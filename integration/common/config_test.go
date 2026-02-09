package common

import (
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestNormalizeConfigClampsAndDefaults(t *testing.T) {
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
