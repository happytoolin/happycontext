package common

import (
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
)

func TestSamplingDecisionRules(t *testing.T) {
	base := sampleInput{
		Method:     "GET",
		Path:       "/x",
		StatusCode: 200,
		Duration:   10 * time.Millisecond,
		Rate:       0,
	}

	if !shouldWriteEvent(hc.Config{}, sampleInput{HasError: true, StatusCode: 200}) {
		t.Fatal("expected hasError to force logging")
	}
	if !shouldWriteEvent(hc.Config{}, sampleInput{StatusCode: 500}) {
		t.Fatal("expected 5xx to force logging")
	}
	if shouldWriteEvent(hc.Config{}, base) {
		t.Fatal("expected rate 0 healthy request to be dropped")
	}
	if !shouldWriteEvent(hc.Config{}, sampleInput{Rate: 1}) {
		t.Fatal("expected rate 1 to always log")
	}
}

func TestSamplingDecisionUsesLevelOverrides(t *testing.T) {
	cfg := hc.Config{
		SamplingRate:       0,
		LevelSamplingRates: map[hc.Level]float64{hc.LevelWarn: 1},
	}

	if !shouldWriteEvent(cfg, sampleInput{StatusCode: 200, Level: hc.LevelWarn}) {
		t.Fatal("expected warn-level override to force logging")
	}
	if shouldWriteEvent(cfg, sampleInput{StatusCode: 200, Level: hc.LevelInfo}) {
		t.Fatal("expected info level to use default rate")
	}
}

func TestSamplingDecisionUsesCustomSampler(t *testing.T) {
	cfg := hc.Config{
		Sampler: func(in hc.SampleInput) bool {
			return in.Level == hc.LevelWarn || in.Path == "/always"
		},
	}

	if !shouldWriteEvent(cfg, sampleInput{Path: "/x", Level: hc.LevelWarn}) {
		t.Fatal("expected custom sampler to keep warn level")
	}
	if !shouldWriteEvent(cfg, sampleInput{Path: "/always", Level: hc.LevelInfo}) {
		t.Fatal("expected custom sampler to keep /always")
	}
	if shouldWriteEvent(cfg, sampleInput{Path: "/x", Level: hc.LevelInfo}) {
		t.Fatal("expected custom sampler to drop unmatched events")
	}
}

func TestShouldSampleBounds(t *testing.T) {
	if shouldSample(-1) {
		t.Fatal("negative rate should not sample")
	}
	if shouldSample(0) {
		t.Fatal("zero rate should not sample")
	}
	if !shouldSample(1) {
		t.Fatal("one rate should sample")
	}
	if !shouldSample(2) {
		t.Fatal("rate over one should sample")
	}
}

func TestNextSampleFloat64Range(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := nextSampleFloat64()
		if v < 0 || v >= 1 {
			t.Fatalf("sample %v out of range [0,1)", v)
		}
	}
}
