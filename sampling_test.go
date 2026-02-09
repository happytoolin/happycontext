package hc

import (
	"fmt"
	"testing"
	"time"
)

func TestChainSampler(t *testing.T) {
	s := ChainSampler(
		NeverSampler(),
		KeepErrors(),
		KeepPathPrefix("/admin"),
		KeepSlowerThan(250*time.Millisecond),
	)

	if !s(SampleInput{Path: "/admin/users", StatusCode: 200, Duration: 10 * time.Millisecond}) {
		t.Fatal("expected admin path to be kept")
	}
	if !s(SampleInput{Path: "/api/orders", StatusCode: 500, Duration: 10 * time.Millisecond}) {
		t.Fatal("expected 5xx request to be kept")
	}
	if !s(SampleInput{Path: "/api/orders", StatusCode: 200, Duration: 300 * time.Millisecond}) {
		t.Fatal("expected slow request to be kept")
	}
	if s(SampleInput{Path: "/api/orders", StatusCode: 200, Duration: 10 * time.Millisecond}) {
		t.Fatal("expected fast healthy request to be dropped")
	}
}

func TestKeepSlowerThanNegativeDuration(t *testing.T) {
	s := ChainSampler(NeverSampler(), KeepSlowerThan(-1*time.Second))
	if !s(SampleInput{StatusCode: 200, Duration: 0}) {
		t.Fatal("expected negative threshold to behave like zero")
	}
}

func TestRateSamplerBounds(t *testing.T) {
	if RateSampler(0)(SampleInput{}) {
		t.Fatal("rate 0 should always drop")
	}
	if RateSampler(-1)(SampleInput{}) {
		t.Fatal("negative rate should always drop")
	}
	if !RateSampler(1)(SampleInput{}) {
		t.Fatal("rate 1 should always keep")
	}
	if !RateSampler(2)(SampleInput{}) {
		t.Fatal("rate >1 should always keep")
	}
}

func ExampleChainSampler() {
	sampler := ChainSampler(
		RateSampler(0),
		KeepErrors(),
		KeepPathPrefix("/checkout"),
	)

	fmt.Println(sampler(SampleInput{Path: "/catalog", StatusCode: 200}))
	fmt.Println(sampler(SampleInput{Path: "/checkout/start", StatusCode: 200}))
	fmt.Println(sampler(SampleInput{Path: "/catalog", StatusCode: 503}))

	// Output:
	// false
	// true
	// true
}
