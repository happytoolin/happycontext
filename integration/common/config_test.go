package common

import (
	"testing"

	"github.com/happytoolin/hlog"
)

func TestNormalizeConfigClampsAndDefaults(t *testing.T) {
	tests := []struct {
		name    string
		cfg     hlog.Config
		wantMsg string
		wantRat float64
	}{
		{name: "negative", cfg: hlog.Config{SamplingRate: -1}, wantMsg: DefaultMessage, wantRat: 0},
		{name: "over one", cfg: hlog.Config{SamplingRate: 2}, wantMsg: DefaultMessage, wantRat: 1},
		{name: "custom", cfg: hlog.Config{SamplingRate: 0.5, Message: "done"}, wantMsg: "done", wantRat: 0.5},
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
		})
	}
}

func TestMergeLevelWithFloor(t *testing.T) {
	tests := []struct {
		name         string
		auto         string
		requested    string
		hasRequested bool
		want         string
	}{
		{name: "no request", auto: hlog.LevelInfo, want: hlog.LevelInfo},
		{name: "invalid request", auto: hlog.LevelInfo, requested: "TRACE", hasRequested: true, want: hlog.LevelInfo},
		{name: "raise level", auto: hlog.LevelInfo, requested: hlog.LevelWarn, hasRequested: true, want: hlog.LevelWarn},
		{name: "keep floor", auto: hlog.LevelError, requested: hlog.LevelDebug, hasRequested: true, want: hlog.LevelError},
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
