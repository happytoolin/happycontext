package hc

// Agent G — time and metadata abuse. Extreme time.Time values,
// hostile OperationStart metadata (domains, names), and duration
// extremes must never crash the pipeline and must stay wire-parseable.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestCrashExtremeTimes(t *testing.T) {
	times := []time.Time{
		time.Time{}, // zero
		time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC),      // negative year
		time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),       // year 0
		time.Date(12345, 6, 7, 8, 9, 10, 11, time.UTC), // five-digit year
		time.Date(-100000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Unix(math.MinInt64, 0),                         // wall-clock wrapped
		time.Unix(math.MaxInt64, math.MaxInt32),             // max nano
		time.Now().Add(math.MaxInt64),                       // overflowed Add
		time.Date(2024, 13, 45, 30, 70, 70, 1e10, time.UTC), // normalized
	}
	for i, ts := range times {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("time[%d] %v panicked: %v", i, ts, r)
				}
			}()
			rec := recOf(LevelInfo, "t", fieldOf("t", ts), fieldOf("t2", ts))
			_ = mustParseLine(t, rec.Encoded())

			rt, ts2 := testRT(t, nil)
			op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
			Add(op.Context(), "when", ts)
			_ = op.End(nil)
			if len(ts2.Events()) != 1 {
				t.Fatalf("time[%d] dropped", i)
			}
		}()
	}
}

func TestCrashExtremeDurations(t *testing.T) {
	durs := []time.Duration{
		0,
		-1,
		time.Nanosecond,
		math.MaxInt64,
		math.MinInt64,
		time.Duration(math.MaxInt64 / 2), // near-max without const overflow
		-24 * time.Hour,
	}
	for i, d := range durs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("dur[%d] %v panicked: %v", i, d, r)
				}
			}()
			rec := recOf(LevelInfo, "d", fieldOf("d", d))
			m := mustParseLine(t, rec.Encoded())
			if m["d"] == nil {
				t.Fatalf("dur[%d] vanished", i)
			}
		}()
	}
	// duration_ms annotation of an extremely long operation clock read
	// is bounded by real time; the field constructor path above covers
	// the value extremes.
}

func TestCrashHostileOperationStart(t *testing.T) {
	starts := []OperationStart{
		{Domain: DomainHTTP, Name: strings.Repeat("n", 1<<16)}, // 64KB name
		{Domain: "custom-domain\nwith\"hostile\x00bytes", Name: "x"},
		{Domain: Domain(strings.Repeat("d", 4096)), Name: "x"},
		{Domain: DomainJob, Name: "j", ID: "id\xff\xfe", Source: "src\x00"},
		{Domain: "MIXEDcase", Name: "x"}, // normalizeDomain case handling
		{Domain: DomainJob, Name: "j", Attempt: -5, MaxAttempts: -5},
		{Domain: DomainJob, Name: "j", Attempt: math.MaxInt32, MaxAttempts: math.MinInt32},
	}
	for i, st := range starts {
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, st)
		Add(op.Context(), "i", i)
		if !op.End(nil) {
			t.Fatalf("start[%d] dropped", i)
		}
		m := mustParseLine(t, []byte(encodedOf(t, ts)))
		if m["op.outcome"] == nil {
			t.Fatalf("start[%d]: outcome missing", i)
		}
	}
}

// encodedOf renders the single captured event via the JSON sink shape.
func encodedOf(t *testing.T, ts *TestSink) string {
	t.Helper()
	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	rec := recOf(evs[0].Level(), evs[0].Message(), evs[0].Fields()...)
	return string(rec.Encoded())
}

// Domain policy lookup semantics, pinned: the "" alias installs the
// default-domain ("operation") policy and applies ONLY there; domains
// are exact-match (case-sensitive); explicit keys beat the alias for
// the same normalized slot.
func TestCrashDomainPolicyLookupShapes(t *testing.T) {
	mk := func(pol map[Domain]OperationPolicy) (*Runtime, *TestSink) {
		sink := NewTestSink()
		return MustCompile(Config{Sink: sink, SamplingRate: 1, OperationPolicies: pol}), sink
	}

	// Alias applies to the default domain only.
	rtAlias, sink := mk(map[Domain]OperationPolicy{"": {SuccessLevel: LevelWarn}})
	for _, d := range []Domain{"", "operation"} {
		op := Start(context.Background(), rtAlias, OperationStart{Domain: d, Name: "x"})
		_ = op.End(nil)
	}
	if got := len(sink.Events()); got != 2 {
		t.Fatalf("events = %d", got)
	}
	for _, ev := range sink.Events() {
		if ev.Level() != LevelWarn {
			t.Fatalf("alias domain level = %v, want warn", ev.Level())
		}
	}
	// Non-default domains do NOT inherit the alias.
	op := Start(context.Background(), rtAlias, OperationStart{Domain: DomainJob, Name: "x"})
	_ = op.End(nil)
	if got := sink.Events()[2].Level(); got != LevelInfo {
		t.Fatalf("job inherited alias: level = %v, want info", got)
	}

	// Domains are exact-match, case-sensitive.
	rtCase, sinkCase := mk(map[Domain]OperationPolicy{"JoB": {SuccessLevel: LevelError}})
	op = Start(context.Background(), rtCase, OperationStart{Domain: "JoB", Name: "x"})
	_ = op.End(nil)
	if got := sinkCase.Events()[0].Level(); got != LevelError {
		t.Fatalf("JoB level = %v, want error", got)
	}
	op = Start(context.Background(), rtCase, OperationStart{Domain: DomainJob, Name: "x"})
	_ = op.End(nil)
	if got := sinkCase.Events()[1].Level(); got != LevelInfo {
		t.Fatalf("job matched JoB policy: level = %v, want info", got)
	}

	// Explicit default key beats the alias deterministically.
	rtBoth, sinkBoth := mk(map[Domain]OperationPolicy{"": {SuccessLevel: LevelInfo}, "operation": {SuccessLevel: LevelError}})
	op = Start(context.Background(), rtBoth, OperationStart{Domain: "", Name: "x"})
	_ = op.End(nil)
	if got := sinkBoth.Events()[0].Level(); got != LevelError {
		t.Fatalf("explicit operation level = %v, want error (explicit beats alias)", got)
	}
}

func TestCrashSetRouteHostile(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
	SetRoute(op.Context(), "/x/\xff\xfe\"\nroute")
	Add(op.Context(), "ok", 1)
	_ = op.End(nil)
	m := mustParseLine(t, []byte(encodedOf(t, ts)))
	if m["ok"] != float64(1) {
		t.Fatalf("sibling lost: %v", m)
	}
	_ = fmt.Sprint()
}
