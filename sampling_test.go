package hc

import (
	"math"
	"testing"
	"time"
)

func sampleIn(path string, hasError bool) SampleInput {
	return SampleInput{
		Domain:     DomainHTTP,
		Operation:  "GET /x",
		Outcome:    OutcomeSuccess,
		StatusCode: 200,
		Path:       path,
		Duration:   10 * time.Millisecond,
		Level:      LevelInfo,
		HasError:   hasError,
	}
}

func TestSamplerHelpers(t *testing.T) {
	base := func(SampleInput) bool { return false }

	if !KeepErrors()(base)(sampleIn("/x", true)) {
		t.Fatal("KeepErrors dropped an error")
	}
	if KeepErrors()(base)(sampleIn("/x", false)) {
		t.Fatal("KeepErrors kept a healthy event past base")
	}
	if !KeepSlowerThan(5 * time.Millisecond)(base)(sampleIn("/x", false)) {
		t.Fatal("KeepSlowerThan dropped a slow event")
	}
	if KeepPathPrefix("/health")(base)(sampleIn("/api", false)) {
		t.Fatal("KeepPathPrefix kept a non-matching path")
	}
	if !KeepPathPrefix("/api")(base)(sampleIn("/api/v1", false)) {
		t.Fatal("KeepPathPrefix dropped a matching prefix")
	}
	if KeepPathPrefix()(base)(sampleIn("/api", false)) {
		t.Fatal("empty KeepPathPrefix must pass through to base (drop)")
	}
}

func TestChainSamplerOrder(t *testing.T) {
	calls := []string{}
	mk := func(name string) SamplerMiddleware {
		return func(next Sampler) Sampler {
			return func(in SampleInput) bool {
				calls = append(calls, name)
				return next(in)
			}
		}
	}
	chained := ChainSampler(AlwaysSampler(), mk("a"), mk("b"))
	if !chained(sampleIn("/x", false)) {
		t.Fatal("chained sampler dropped")
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Fatalf("middleware order = %v, want [a b] (a wraps b)", calls)
	}
}

func TestRateSamplerBoundaries(t *testing.T) {
	if RateSampler(-1)(sampleIn("/x", false)) {
		t.Fatal("negative rate kept")
	}
	if RateSampler(0)(sampleIn("/x", false)) {
		t.Fatal("zero rate kept")
	}
	if RateSampler(2)(sampleIn("/x", false)) == false {
		t.Fatal("rate >= 1 dropped")
	}
	// NaN never keeps
	if RateSampler(math.NaN())(sampleIn("/x", false)) {
		t.Fatal("NaN rate kept")
	}
}

func TestSampleInputSyntheticLookup(t *testing.T) {
	in := SampleInput{}
	if _, ok := in.Lookup("k"); ok {
		t.Fatal("synthetic input found a key")
	}
	if in.Fields() != nil {
		t.Fatal("synthetic input returned fields")
	}
}
