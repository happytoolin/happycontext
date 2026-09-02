package hc

// Runtime compilation, metadata, sampling, and fan-out tests

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- from runtime_test.go ---
func TestCompileValid(t *testing.T) {
	rate := 0.25
	rt, err := Compile(Config{
		Sink:               NewTestSink(),
		SamplingRate:       1,
		LevelSamplingRates: map[Level]float64{LevelDebug: 0.1},
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {SuccessLevel: LevelDebug, SamplingRate: &rate},
		},
		Message: "done",
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if rt.noop() {
		t.Fatal("valid sink compiled to noop")
	}
	if rt.message != "done" {
		t.Errorf("message = %q", rt.message)
	}
}

func TestCompileNilSinkIsValidNoop(t *testing.T) {
	rt, err := Compile(Config{})
	if err != nil {
		t.Fatalf("nil sink must be valid: %v", err)
	}
	if !rt.noop() {
		t.Fatal("nil sink must be a no-op runtime")
	}
}

func TestCompileSentinels(t *testing.T) {
	cases := []struct {
		name  string
		cfg   Config
		sent  error
		inMsg string
	}{
		{
			name:  "rate above one",
			cfg:   Config{SamplingRate: 1.5},
			sent:  ErrInvalidRate,
			inMsg: "hc: sampling rate 1.5",
		},
		{
			name:  "rate negative",
			cfg:   Config{SamplingRate: -0.1},
			sent:  ErrInvalidRate,
			inMsg: "hc: sampling rate -0.1",
		},
		{
			name: "rate NaN",
			cfg:  Config{SamplingRate: math.NaN()},
			sent: ErrInvalidRate,
		},
		{
			name: "level rate bad level key",
			cfg:  Config{LevelSamplingRates: map[Level]float64{Level(3): 0.5}},
			sent: ErrInvalidLevel,
		},
		{
			name: "level rate bad rate",
			cfg:  Config{LevelSamplingRates: map[Level]float64{LevelWarn: 2}},
			sent: ErrInvalidRate,
		},
		{
			name: "policy bad success level",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {SuccessLevel: Level(7)}}},
			sent: ErrInvalidLevel,
		},
		{
			name: "policy bad outcome key",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {OutcomeLevels: map[Outcome]Level{Outcome("nope"): LevelWarn}}}},
			sent: ErrInvalidOutcome,
		},
		{
			name: "policy bad rate",
			cfg:  Config{OperationPolicies: map[Domain]OperationPolicy{DomainJob: {SamplingRate: floatPtr(1.2)}}},
			sent: ErrInvalidRate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Compile(c.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, c.sent) {
				t.Fatalf("error %v does not wrap sentinel %v", err, c.sent)
			}
			if c.inMsg != "" && err.Error()[:len(c.inMsg)] != c.inMsg {
				t.Fatalf("error %q lacks %q prefix", err, c.inMsg)
			}
		})
	}
}

func TestMustCompilePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustCompile should panic on bad config")
		}
	}()
	MustCompile(Config{SamplingRate: 2})
}

func TestMustCompileLiteralOK(t *testing.T) {
	rt := MustCompile(Config{Sink: NewTestSink(), SamplingRate: 1})
	if rt == nil {
		t.Fatal("nil runtime")
	}
}

func TestLevelString(t *testing.T) {
	cases := map[Level]string{
		LevelDebug: "DEBUG",
		LevelInfo:  "INFO",
		LevelWarn:  "WARN",
		LevelError: "ERROR",
		Level(42):  "INFO", // unknown renders as info, wire unchanged
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", int(level), got, want)
		}
	}
}

// TestLevelConstantCompatibility pins the slog ranks so v0 source that
// references the constants keeps compiling and behaving identically.
func TestLevelConstantCompatibility(t *testing.T) {
	if LevelDebug != -4 || LevelInfo != 0 || LevelWarn != 4 || LevelError != 8 {
		t.Fatal("level ranks drifted from the slog-compatible values")
	}
	if !IsValidLevel(LevelWarn) || IsValidLevel(Level(3)) {
		t.Fatal("IsValidLevel misbehaves")
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestPolicyForDomain(t *testing.T) {
	rt := MustCompile(Config{
		OperationPolicies: map[Domain]OperationPolicy{
			DomainJob: {SuccessLevel: LevelDebug},
		},
	})
	if p := rt.policyFor(DomainJob); p.SuccessLevel != LevelDebug {
		t.Errorf("job policy = %+v", p)
	}
	if p := rt.policyFor(DomainHTTP); p.SuccessLevel != LevelInfo {
		t.Errorf("unconfigured policy not zero: %+v", p)
	}
	// empty-string domain normalizes to the default domain
	rt2 := MustCompile(Config{
		OperationPolicies: map[Domain]OperationPolicy{"": {SuccessLevel: LevelWarn}},
	})
	if p := rt2.policyFor(Domain("")); p.SuccessLevel != LevelWarn {
		t.Errorf("alias policy = %+v", p)
	}
}

// --- from metadata_test.go ---
type wrappedTestError struct{ err error }

func (w wrappedTestError) Error() string { return "wrapped: " + w.err.Error() }
func (w wrappedTestError) Unwrap() error { return w.err }

type frameworkStyleTestError struct {
	Code    int
	Message string
}

func (f *frameworkStyleTestError) Error() string {
	return fmt.Sprintf("framework %d: %s", f.Code, f.Message)
}

func TestStructuredErrorField(t *testing.T) {
	field := structuredErrorField(errors.New("boom"))
	if field["message"] != "boom" {
		t.Errorf("message = %v", field["message"])
	}
	if field["type"] != "*errors.errorString" {
		t.Errorf("type = %v", field["type"])
	}
	if _, hasCause := field["cause.message"]; hasCause {
		t.Error("simple error should have no cause")
	}

	wrapped := structuredErrorField(wrappedTestError{err: errors.New("inner")})
	if wrapped["message"] != "wrapped: inner" {
		t.Errorf("message = %v", wrapped["message"])
	}
	if wrapped["cause.message"] != "inner" {
		t.Errorf("cause.message = %v", wrapped["cause.message"])
	}

	fw := structuredErrorField(&frameworkStyleTestError{Code: 500, Message: "kaput"})
	if fw["message"] != "kaput" {
		t.Errorf("framework message = %v", fw["message"])
	}
}

func TestStructuredPanicField(t *testing.T) {
	field := structuredPanicField("boom")
	if field["value"] != "boom" || field["type"] != "string" {
		t.Fatalf("panic field = %v", field)
	}
}

func TestCyclicErrorUnwrap(t *testing.T) {
	a := &cyclicError{}
	b := &cyclicError{next: a}
	a.next = b
	field := structuredErrorField(a)
	if field == nil {
		t.Fatal("cyclic unwrap must terminate")
	}
}

type cyclicError struct{ next error }

func (c *cyclicError) Error() string { return "cyclic" }
func (c *cyclicError) Unwrap() error { return c.next }

// --- from sampling_test.go ---
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

// --- from fanout_test.go ---
// FanoutSink writes every record to each member sink in order. If a
// member panics, the remaining members still receive the record and
// the panic propagates to the caller (who decides what to do with it).
type FanoutSink struct {
	sinks []Sink
}

// NewFanoutSink creates a fan-out over the given sinks.
func NewFanoutSink(sinks ...Sink) *FanoutSink {
	return &FanoutSink{sinks: sinks}
}

// Write delivers the record to every member sink, continuing past
// panicking members (their panic is re-raised after the fan-out so the
// caller sees the first failure).
func (f *FanoutSink) Write(ctx context.Context, rec *Record) {
	if f == nil {
		return
	}
	var firstPanic any
	for _, s := range f.sinks {
		func() {
			defer func() {
				if r := recover(); r != nil && firstPanic == nil {
					firstPanic = r
				}
			}()
			s.Write(ctx, rec)
		}()
	}
	if firstPanic != nil {
		panic(firstPanic)
	}
}

var _ Sink = (*FanoutSink)(nil)

// captureSink records every record it receives (deep copy via the
// shared TestSink machinery).
type fanCaptureSink struct {
	mu     sync.Mutex
	events []CapturedEvent
}

func (s *fanCaptureSink) Write(_ context.Context, rec *Record) {
	if rec == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, CapturedEvent{
		level:   rec.Level(),
		message: rec.Message(),
		fields:  copyFields(rec.Fields()),
	})
}

func (s *fanCaptureSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func (s *fanCaptureSink) last() CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events[len(s.events)-1]
}

// panicSink panics on Write.
type panicSink struct{}

func (panicSink) Write(context.Context, *Record) { panic("sink exploded") }

// TestFanoutSinkPanicIsolation: one panicking member must not stop the
// others from receiving the record.
func TestFanoutSinkPanicIsolation(t *testing.T) {
	for _, n := range []int{2, 4, 8} {
		for _, panicIdx := range []int{0, n / 2, n - 1} {
			t.Run(fmt.Sprintf("n=%d panic=%d", n, panicIdx), func(t *testing.T) {
				captures := make([]*fanCaptureSink, n)
				sinks := make([]Sink, n)
				for i := range sinks {
					captures[i] = &fanCaptureSink{}
					sinks[i] = captures[i]
				}
				sinks[panicIdx] = panicSink{}
				ts := NewFanoutSink(sinks...)
				rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "fan"})
				Add(op.Context(), "k", "v")

				var escaped any
				func() {
					defer func() { escaped = recover() }()
					op.End(nil)
				}()
				if escaped == nil {
					t.Fatal("the panicking sink's panic did not escape")
				}
				// every non-panicking member received the record
				for i, c := range captures {
					if i == panicIdx {
						continue // this slot holds the panicking sink
					}
					if c.count() != 1 {
						t.Fatalf("sink %d captured %d events", i, c.count())
					}
					if v, _ := c.last().Lookup("k"); v != "v" {
						t.Fatalf("sink %d event corrupted", i)
					}
				}
				// pool clean after the panic
				ok := &fanCaptureSink{}
				rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
				op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
				Add(op2.Context(), "clean", true)
				op2.End(nil)
				if ok.count() != 1 {
					t.Fatal("pool corrupted after the panicking fan-out")
				}
			})
		}
	}
}

// TestFanoutAllPanicking: when every member panics the caller sees the
// first panic, and the request still does not corrupt the pool.
func TestFanoutAllPanicking(t *testing.T) {
	for _, n := range []int{2, 4} {
		sinks := make([]Sink, n)
		for i := range sinks {
			sinks[i] = panicSink{}
		}
		rt := MustCompile(Config{Sink: NewFanoutSink(sinks...), SamplingRate: 1})
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "all-panic"})
		Add(op.Context(), "k", 1)
		var escaped any
		func() {
			defer func() { escaped = recover() }()
			op.End(nil)
		}()
		if escaped == nil || escaped != "sink exploded" {
			t.Fatalf("escaped %v", escaped)
		}
		// pool clean: the next request works
		ok := &fanCaptureSink{}
		rt2 := MustCompile(Config{Sink: ok, SamplingRate: 1})
		op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "after"})
		op2.End(nil)
		if ok.count() != 1 {
			t.Fatal("pool corrupted after all-panic fan-out")
		}
	}
}

// TestFanoutRetainingSink: a member that retains the Record past Write
// must copy it (amendment 9) — a buggy retaining member would observe
// recycled memory, which the pool-safety tests pin elsewhere; here we
// pin that the fan-out passes the SAME record view to every member.
func TestFanoutSinkOrder(t *testing.T) {
	a, b := &fanCaptureSink{}, &fanCaptureSink{}
	rt := MustCompile(Config{Sink: NewFanoutSink(a, b), SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "order"})
	Add(op.Context(), "k", "v")
	op.End(nil)
	la, lb := a.last(), b.last()
	if la.Message() != lb.Message() || la.Level() != lb.Level() {
		t.Fatalf("members disagree: %+v vs %+v", la, lb)
	}
	if !strings.Contains(fmt.Sprint(la.Lookup("k")), "v") {
		t.Fatalf("first member lost the field")
	}
	if _, ok := lb.Lookup("k"); !ok {
		t.Fatal("second member lost the field")
	}
}
