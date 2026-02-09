package common

import (
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
)

func TestSamplingDecisionRules(t *testing.T) {
	base := hc.SampleInput{
		Method:     "GET",
		Path:       "/x",
		StatusCode: 200,
		Duration:   10 * time.Millisecond,
		Rate:       0,
	}

	if !shouldWriteEvent(hc.SampleInput{HasError: true, StatusCode: 200}) {
		t.Fatal("expected hasError to force logging")
	}
	if !shouldWriteEvent(hc.SampleInput{StatusCode: 500}) {
		t.Fatal("expected 5xx to force logging")
	}
	if shouldWriteEvent(base) {
		t.Fatal("expected rate 0 healthy request to be dropped")
	}
	if !shouldWriteEvent(hc.SampleInput{Rate: 1}) {
		t.Fatal("expected rate 1 to always log")
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
