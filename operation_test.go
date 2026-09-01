package hc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func testRT(t *testing.T, mut func(*Config)) (*Runtime, *TestSink) {
	t.Helper()
	cfg := Config{Sink: NewTestSink(), SamplingRate: 1}
	if mut != nil {
		mut(&cfg)
	}
	rt, err := Compile(cfg)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ts, _ := rt.sink.(*TestSink)
	return rt, ts
}

func TestLifecycleBasicEmit(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "import", ID: "j-1"})
	ctx := op.Context()
	Add(ctx, "user_id", "u_1", "attempt_no", 2)
	Add(ctx, "took", 1500*time.Millisecond)
	var err error
	err = errors.New("db down")
	if !op.End(&err) {
		t.Fatal("event not emitted")
	}

	events := ts.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	ev := events[0]
	if ev.Level() != LevelError {
		t.Errorf("level = %v", ev.Level())
	}
	if ev.Message() != "operation_completed" {
		t.Errorf("message = %q", ev.Message())
	}
	checks := map[string]any{
		"op.domain":   "job",
		"op.name":     "import",
		"op.id":       "j-1",
		"user_id":     "u_1",
		"attempt_no":  int64(2),
		"took":        1500 * time.Millisecond,
		"op.outcome":  "failure",
		"duration_ms": int64(0), // overwritten below with presence check
	}
	for k, want := range checks {
		got, ok := ev.Lookup(k)
		if !ok {
			t.Errorf("missing field %q", k)
			continue
		}
		if k == "duration_ms" {
			if _, isInt := got.(int64); !isInt {
				t.Errorf("duration_ms = %T", got)
			}
			continue
		}
		if got != want {
			t.Errorf("%q = %v, want %v", k, got, want)
		}
	}
	if errField, ok := ev.Lookup("error"); !ok {
		t.Error("missing structured error field")
	} else if m := errField.(map[string]any); m["message"] != "db down" {
		t.Errorf("error.message = %v", m["message"])
	}
}

func TestLifecycleHTTPDefaults(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "GET /x"})
	Add(op.Context(), "http.method", "GET", "http.path", "/x", "http.status", 204)
	op.End(nil)

	ev := ts.Events()[0]
	if ev.Message() != "request_completed" {
		t.Errorf("message = %q, want request_completed", ev.Message())
	}
	if v, _ := ev.Lookup("op.outcome"); v != "success" {
		t.Errorf("outcome = %v", v)
	}
	if _, hasOpCode := ev.Lookup("op.code"); hasOpCode {
		t.Error("op.code must not be emitted for HTTP operations (http.status carries it)")
	}
	if v, _ := ev.Lookup("http.status"); v != int64(204) {
		t.Errorf("http.status = %v", v)
	}
	if ev.Level() != LevelInfo {
		t.Errorf("level = %v", ev.Level())
	}
}

func TestLifecycleOneShotEnd(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{})
	first := op.End(nil)
	second := op.End(nil)
	third := op.End(nil)
	if !first || second != first || third != first {
		t.Fatalf("one-shot violated: %v %v %v", first, second, third)
	}
	if len(ts.Events()) != 1 {
		t.Fatalf("events = %d, want 1", len(ts.Events()))
	}
	// pool state intact: a fresh request still works
	op2 := Start(context.Background(), rt, OperationStart{})
	if !op2.End(nil) {
		t.Fatal("second request dropped")
	}
	if len(ts.Events()) != 2 {
		t.Fatalf("events = %d, want 2", len(ts.Events()))
	}
}

func TestLifecyclePanicCapturedAndRepanicked(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "boom"})

	func() {
		defer func() { recover() }() // swallows End's re-panic
		defer op.End(nil)            // direct defer: observes the panic
		panic("kaboom")
	}()

	if len(ts.Events()) != 1 {
		t.Fatal("panic event not emitted")
	}
	ev := ts.Events()[0]
	if v, _ := ev.Lookup("op.outcome"); v != "panic" {
		t.Errorf("outcome = %v", v)
	}
	if p, ok := ev.Lookup("panic"); !ok {
		t.Error("missing panic field")
	} else if pm := p.(map[string]any); pm["value"] != "kaboom" {
		t.Errorf("panic.value = %v", pm["value"])
	}
	if ev.Level() != LevelError {
		t.Errorf("level = %v", ev.Level())
	}
}

// deferred End observes the panic itself and must re-panic after commit
func TestLifecycleRepanic(t *testing.T) {
	rt, _ := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{})
	repanicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				repanicked = true
				if r != "original" {
					t.Errorf("re-panic value = %v", r)
				}
			}
		}()
		defer op.End(nil)
		panic("original")
	}()
	if !repanicked {
		t.Fatal("End swallowed the panic")
	}
}

func TestOutcomePrecedence(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(ctx context.Context)
		err      error
		panicked any
		want     Outcome
	}{
		{"success", func(context.Context) {}, nil, nil, OutcomeSuccess},
		{"panic beats error", func(context.Context) {}, errors.New("e"), "p", OutcomePanic},
		{"error", func(context.Context) {}, errors.New("e"), nil, OutcomeFailure},
		{"canceled", func(context.Context) {}, context.Canceled, nil, OutcomeCanceled},
		{"timeout", func(context.Context) {}, context.DeadlineExceeded, nil, OutcomeTimeout},
		{"explicit beats 5xx", func(ctx context.Context) {
			Add(ctx, "op.outcome", "retry")
			Add(ctx, "http.status", 503)
		}, nil, nil, OutcomeRetry},
		{"error beats explicit", func(ctx context.Context) { Add(ctx, "op.outcome", "retry") }, errors.New("e"), nil, OutcomeFailure},
		{"5xx status", func(ctx context.Context) { Add(ctx, "http.status", 500) }, nil, nil, OutcomeFailure},
		{"4xx is success", func(ctx context.Context) { Add(ctx, "http.status", 404) }, nil, nil, OutcomeSuccess},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt, ts := testRT(t, nil)
			op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "x"})
			c.setup(op.Context())
			if c.panicked != nil {
				func() {
					defer func() { recover() }()
					defer op.End(nil)
					panic(c.panicked)
				}()
			} else {
				op.End(&c.err)
			}
			got, _ := ts.Events()[0].Lookup("op.outcome")
			if Outcome(got.(string)) != c.want {
				t.Fatalf("outcome = %v, want %v", got, c.want)
			}
		})
	}
}

// TestErrorsBypassSampling is amendment 4: failures are never sampled
// away, structurally, before any custom sampler runs.
func TestErrorsBypassSampling(t *testing.T) {
	samplerCalls := 0
	rt, ts := testRT(t, func(c *Config) {
		c.Sampler = func(in SampleInput) bool {
			samplerCalls++
			return false
		}
	})
	_ = rt

	// failing request still emits; the sampler never ran
	op := Start(context.Background(), rt, OperationStart{})
	var err error = errors.New("boom")
	if !op.End(&err) {
		t.Fatal("error event was sampled away")
	}
	if samplerCalls != 0 {
		t.Fatalf("custom sampler ran %d times on an error event", samplerCalls)
	}
	if len(ts.Events()) != 1 {
		t.Fatal("error event not written")
	}

	// healthy request is dropped by the sampler
	op2 := Start(context.Background(), rt, OperationStart{})
	if op2.End(nil) {
		t.Fatal("healthy event survived NeverSampler")
	}
	if samplerCalls != 1 {
		t.Fatalf("sampler calls = %d, want 1", samplerCalls)
	}
}

func TestSampleInputLookupAndFields(t *testing.T) {
	var seenTier any
	var seenCount int
	rt, ts := testRT(t, func(c *Config) {
		c.Sampler = func(in SampleInput) bool {
			seenCount++
			if v, ok := in.Lookup("user_tier"); ok {
				seenTier = v
			}
			for _, f := range in.Fields() {
				if f.Key() == "counter" {
					seenCount += 0
				}
			}
			return in.Outcome == OutcomeSuccess || in.HasError
		}
	})
	op := Start(context.Background(), rt, OperationStart{})
	Add(op.Context(), "user_tier", "enterprise")
	op.End(nil)

	if seenTier != "enterprise" {
		t.Fatalf("Lookup(user_tier) = %v", seenTier)
	}
	if len(ts.Events()) != 1 {
		t.Fatal("success event dropped by its own sampler")
	}
}

// TestNonHTTPOpCode pins the canonical-field rule: op.code is non-HTTP
// only, surfaced from the explicit op.code field the caller wrote.
func TestNonHTTPOpCode(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "import"})
	Add(op.Context(), "op.code", 42)
	op.End(nil)
	ev := ts.Events()[0]
	if v, _ := ev.Lookup("op.code"); v != int64(42) {
		t.Fatalf("op.code = %v, want 42", v)
	}

	// absent when not set
	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "x"})
	op2.End(nil)
	if _, has := ts.Events()[1].Lookup("op.code"); has {
		t.Fatal("op.code emitted without an explicit field")
	}
}

// TestExplicitOpCodeOnHTTPIsUserData pins the boundary: the core never
// SYNTHESIZES op.code for HTTP operations, but an explicit user write
// is their data and survives.
func TestExplicitOpCodeOnHTTPIsUserData(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "GET /x"})
	Add(op.Context(), "http.status", 200)
	op.End(nil)
	if _, has := ts.Events()[0].Lookup("op.code"); has {
		t.Fatal("core synthesized op.code for an HTTP operation")
	}

	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "GET /x"})
	Add(op2.Context(), "http.status", 200, "op.code", 777)
	op2.End(nil)
	if v, has := ts.Events()[1].Lookup("op.code"); !has || v != int64(777) {
		t.Fatalf("explicit user op.code dropped: %v %v", v, has)
	}
}

func TestRequestedLevelFloor(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{})
	SetLevel(op.Context(), LevelWarn) // success auto-level is info
	op.End(nil)
	if ev := ts.Events()[0]; ev.Level() != LevelWarn {
		t.Fatalf("level = %v, want warn floor", ev.Level())
	}
}

func TestSetMessageOverride(t *testing.T) {
	rt, ts := testRT(t, func(c *Config) { c.Message = "configured" })
	op := Start(context.Background(), rt, OperationStart{})
	SetMessage(op.Context(), "handler message")
	op.End(nil)
	if ev := ts.Events()[0]; ev.Message() != "handler message" {
		t.Fatalf("message = %q", ev.Message())
	}

	op2 := Start(context.Background(), rt, OperationStart{})
	op2.End(nil)
	if ev := ts.Events()[1]; ev.Message() != "configured" {
		t.Fatalf("configured message = %q", ev.Message())
	}
}

func TestNilRuntimeNoop(t *testing.T) {
	var rt *Runtime
	op := Start(context.Background(), rt, OperationStart{})
	Add(op.Context(), "k", 1) // requests run
	if op.End(nil) {
		t.Fatal("nil runtime emitted")
	}
	// and nothing panicked
}

func TestAddNoEventNoop(t *testing.T) {
	Add(context.Background(), "k", 1) // no panic
	Add(nil, "k", 1)
	AddRawJSON(context.Background(), "k", []byte(`{}`))
	Error(context.Background(), errors.New("x"))
	SetMessage(context.Background(), "m")
	SetRoute(context.Background(), "/r")
	SetLevel(context.Background(), LevelWarn)
}

func TestAddRawJSONField(t *testing.T) {
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{})
	AddRawJSON(op.Context(), "meta", []byte(`{"nested":true}`))
	op.End(nil)
	ev := ts.Events()[0]
	var found *Field
	for _, cand := range ev.Fields() {
		if cand.Key() == "meta" {
			f := cand
			found = &f
		}
	}
	if found == nil {
		t.Fatal("raw field missing")
	}
	if found.Kind() != KindRaw {
		t.Fatalf("kind = %v", found.Kind())
	}
	if raw, ok := found.Raw(); !ok || string(raw) != `{"nested":true}` {
		t.Fatalf("raw = %v", raw)
	}
}

func TestLevelSamplingRates(t *testing.T) {
	rt, ts := testRT(t, func(c *Config) {
		c.SamplingRate = 0
		c.LevelSamplingRates = map[Level]float64{LevelWarn: 1}
	})
	// info events dropped (rate 0), warn kept via level rate
	op := Start(context.Background(), rt, OperationStart{})
	if op.End(nil) {
		t.Fatal("info event kept despite rate 0")
	}
	op2 := Start(context.Background(), rt, OperationStart{})
	SetLevel(op2.Context(), LevelWarn)
	if !op2.End(nil) {
		t.Fatal("warn event dropped despite level rate 1")
	}
	if len(ts.Events()) != 1 {
		t.Fatalf("events = %d", len(ts.Events()))
	}
}

func TestDomainPolicySamplingRate(t *testing.T) {
	rate := 1.0
	rt, ts := testRT(t, func(c *Config) {
		c.SamplingRate = 0
		c.OperationPolicies = map[Domain]OperationPolicy{
			DomainJob: {SamplingRate: &rate},
		}
	})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob})
	if !op.End(nil) {
		t.Fatal("job event dropped despite domain rate 1")
	}
	op2 := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP})
	if op2.End(nil) {
		t.Fatal("http event kept despite generic rate 0")
	}
	if len(ts.Events()) != 1 {
		t.Fatalf("events = %d", len(ts.Events()))
	}
}

func TestOperationContextReachesSink(t *testing.T) {
	type ctxKey struct{}
	var gotCtx context.Context
	rt := MustCompile(Config{
		Sink:         sinkFunc(func(ctx context.Context, rec *Record) { gotCtx = ctx }),
		SamplingRate: 1,
	})
	parent := context.WithValue(context.Background(), ctxKey{}, "request-value")
	op := Start(parent, rt, OperationStart{})
	op.End(nil)
	if gotCtx.Value(ctxKey{}) != "request-value" {
		t.Fatal("sink did not receive the request context")
	}
}

type sinkFunc func(context.Context, *Record)

func (f sinkFunc) Write(ctx context.Context, rec *Record) { f(ctx, rec) }

func TestSinkPanicDoesNotLeakPool(t *testing.T) {
	rt, _ := testRT(t, func(c *Config) {
		c.Sink = sinkFunc(func(context.Context, *Record) { panic("sink exploded") })
	})
	func() {
		defer func() { _ = recover() }()
		op := Start(context.Background(), rt, OperationStart{})
		defer op.End(nil)
	}()
	// pool must still be usable
	rt2, ts := testRT(t, nil)
	op := Start(context.Background(), rt2, OperationStart{})
	if !op.End(nil) || len(ts.Events()) != 1 {
		t.Fatal("pool corrupted after sink panic")
	}
}

func TestConcurrentRequests(t *testing.T) {
	rt, ts := testRT(t, nil)
	const goroutines = 16
	const each = 50
	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < each; i++ {
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j", ID: "g"})
				ctx := op.Context()
				Add(ctx, "g", g, "i", i)
				Add(ctx, strings.Repeat("f", 32), i)
				var err error
				if i%7 == 0 {
					err = errors.New("periodic failure")
				}
				op.End(&err)
			}
		}(g)
	}
	for g := 0; g < goroutines; g++ {
		<-done
	}
	events := ts.Events()
	if len(events) != goroutines*each {
		t.Fatalf("events = %d, want %d", len(events), goroutines*each)
	}
	for _, ev := range events {
		if _, ok := ev.Lookup("g"); !ok {
			t.Fatal("event lost its fields — pool corruption")
		}
	}
}
