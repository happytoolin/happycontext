package hc

// P6c properties (dst-research §7.5, action plan P6): sampling
// invariants.
//
//  1. Error bypass: events with any error source (end error, panic,
//     hc.Error, failure outcome) are emitted regardless of sampler and
//     rate — the amendment-4 structural bypass.
//  2. Rate boundaries: healthy events at rate 0 drop deterministically,
//     at rate 1 keep deterministically.
//  3. ChainSampler composition equals the reference union formula:
//     every middleware keeps its predicate or defers to the next, so a
//     chain over a dropping base is the OR of the middlewares'
//     predicates — re-derived here independently of ChainSampler.

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"
)

// TestSamplingErrorBypassProperty drives generated failure and healthy
// programs through a drop-everything runtime (NeverSampler at rate 0):
// failures must still emit — the amendment-4 structural bypass — while
// healthy events drop. The model's error predicate (end error, panic,
// hc.Error, or a non-success outcome) is the oracle.
func TestSamplingErrorBypassProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5A9A1E5eed, 0x8A55E5eed))
	for i := 0; i < 3000; i++ {
		buf := make([]byte, 2+rng.IntN(120))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate0
		m := buildModel(prog)
		outcome := m.outcome()

		sink := &lifeSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: 0, Sampler: NeverSampler()})
		op := Start(context.Background(), rt, OperationStart{Domain: prog.start, Name: "n"})
		executeProgramOn(prog, op)
		got := len(sink.events)
		want := 0
		if m.emitted(outcome) {
			want = 1
		}
		if got != want {
			t.Fatalf("iter %d: drop-everything runtime emitted %d events, want %d (outcome %s errOp=%v endErr=%v panicked=%v)",
				i, got, want, outcome, m.errOp, m.endErr != nil, m.endPanicked)
		}
	}
}

// TestSamplingRateBoundaryProperty pins the deterministic rate edges
// over generated healthy programs: rate 0 drops every healthy event,
// rate 1 keeps every one, and error events are emitted at both.
func TestSamplingRateBoundaryProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xB0A0A5eed, 0xD8A7A5eed))
	run := func(rate float64, prog lifeProgram) int {
		sink := &lifeSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: rate})
		op := Start(context.Background(), rt, OperationStart{Domain: prog.start, Name: "n"})
		executeProgramOn(prog, op)
		return len(sink.events)
	}
	for i := 0; i < 1500; i++ {
		buf := make([]byte, 2+rng.IntN(120))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate0 // runtime selection irrelevant: rate passed explicitly
		m := buildModel(prog)
		outcome := m.outcome()

		// Only programs that reach an End on a live runtime and stay
		// healthy belong here: failures (bypass test) and never-ended
		// streams (no event at any rate) are filtered out.
		if !m.ended || m.mode == modeNilRuntime || m.mode == modeNilSink || m.hasError(outcome) {
			continue
		}
		// healthy program: rate 0 drops, rate 1 keeps
		if got := run(0, prog); got != 0 {
			t.Fatalf("iter %d: healthy event survived rate 0", i)
		}
		if got := run(1, prog); got != 1 {
			t.Fatalf("iter %d: healthy event dropped at rate 1", i)
		}
	}
}

// chainCase describes one generated ChainSampler configuration.
type chainCase struct {
	hasErr    bool
	code      int
	status    int
	duration  time.Duration
	path      string
	keepErr   bool
	minDur    time.Duration
	prefixes  []string
	useSlower bool
	usePrefix bool
}

func chainReference(c chainCase) bool {
	// The reference union: each middleware keeps its predicate or
	// defers to the (dropping) base, so the composed decision is the OR
	// of the middlewares' predicates. KeepSlowerThan clamps negative
	// minimums to zero; KeepPathPrefix filters empty prefixes.
	keep := false
	if c.keepErr && (c.hasErr || c.code >= 500 || c.status >= 500) {
		keep = true
	}
	if c.useSlower {
		min := c.minDur
		if min < 0 {
			min = 0
		}
		if c.duration >= min {
			keep = true
		}
	}
	if c.usePrefix {
		for _, p := range c.prefixes {
			if p != "" && len(c.path) >= len(p) && c.path[:len(p)] == p {
				keep = true
			}
		}
	}
	return keep
}

// TestChainSamplerProperty compares ChainSampler's composed decision
// against the reference union formula over generated inputs.
func TestChainSamplerProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xC4A1A5eed, 0xE5EED5E))
	for i := 0; i < 5000; i++ {
		c := chainCase{
			hasErr:    rng.Uint64()&1 == 0,
			code:      int(rng.IntN(700)),
			status:    int(rng.IntN(700)),
			duration:  time.Duration(rng.IntN(200)-50) * time.Millisecond,
			path:      []string{"", "/", "/api/v1/items", "/healthz", "/other"}[rng.IntN(5)],
			keepErr:   rng.Uint64()&1 == 0,
			minDur:    time.Duration(rng.IntN(100)-20) * time.Millisecond,
			useSlower: rng.Uint64()&1 == 0,
			usePrefix: rng.Uint64()&1 == 0,
		}
		switch rng.IntN(4) {
		case 0:
			c.prefixes = nil
		case 1:
			c.prefixes = []string{"/api"}
		case 2:
			c.prefixes = []string{"", "/healthz"}
		default:
			c.prefixes = []string{"/nope", "", "/api/v1"}
		}

		in := SampleInput{
			Domain: DomainHTTP, Operation: "GET /x", Outcome: OutcomeSuccess,
			Code: c.code, StatusCode: c.status, Duration: c.duration,
			Path: c.path, Level: LevelInfo, HasError: c.hasErr,
		}
		var middlewares []SamplerMiddleware
		if c.keepErr {
			middlewares = append(middlewares, KeepErrors())
		}
		if c.useSlower {
			middlewares = append(middlewares, KeepSlowerThan(c.minDur))
		}
		if c.usePrefix {
			middlewares = append(middlewares, KeepPathPrefix(c.prefixes...))
		}
		// exercise nil and empty middlewares too
		if rng.Uint64()&1 == 0 {
			middlewares = append(middlewares, nil)
		}
		chained := ChainSampler(NeverSampler(), middlewares...)

		want := chainReference(c)
		if got := chained(in); got != want {
			t.Fatalf("iter %d: chain(%+v) = %v, reference = %v", i, c, got, want)
		}
	}
}
