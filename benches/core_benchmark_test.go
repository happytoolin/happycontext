package benches_test

import (
	"context"
	"strconv"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

type discardSink struct{}

func (discardSink) Write(context.Context, *hc.Record) {}

// runtimeFor returns a compiled runtime with the given sink and full
// sampling (kept events), as the gates measure the kept path.
func runtimeFor(sink hc.Sink) *hc.Runtime {
	return hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
}

// BenchmarkWALAddStableKeys measures the single-pair Add gate by
// delta: Start+Add minus Start-only (constant key/value, the gate
// shape). Fresh operations per iteration keep the WAL at steady state —
// no unbounded growth, no pool return noise.
func BenchmarkWALAddStableKeys(b *testing.B) {
	rt := runtimeFor(discardSink{})
	ctx := context.Background()
	b.Run("start_only", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = hc.Start(ctx, rt, hc.OperationStart{Domain: hc.DomainJob, Name: "bench"})
		}
	})
	b.Run("start_add_pair", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			op := hc.Start(ctx, rt, hc.OperationStart{Domain: hc.DomainJob, Name: "bench"})
			hc.Add(op.Context(), "user_id", "u_8472")
		}
	})
}

// BenchmarkWALAddMany measures the variadic multi-pair Add shape.
func BenchmarkWALAddMany(b *testing.B) {
	rt := runtimeFor(discardSink{})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "bench"})
	ctx := op.Context()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		hc.Add(ctx, "a", i, "b", i, "c", i, "d", i, "e", i, "f", i)
		i++
	}
	op.End(nil)
}

// benchmarkFields appends n typed fields to the operation context.
func benchmarkFields(ctx context.Context, n int) {
	hc.Add(ctx, "http.method", "GET")
	hc.Add(ctx, "http.path", "/api/v1/orders/12345")
	hc.Add(ctx, "http.route", "/api/v1/orders/:id")
	hc.Add(ctx, "http.status", 200)
	hc.Add(ctx, "op.domain", "http")
	hc.Add(ctx, "op.name", "GET /api/v1/orders/:id")
	hc.Add(ctx, "op.outcome", "success")
	hc.Add(ctx, "op.code", 200)
	hc.Add(ctx, "duration_ms", 12)
	hc.Add(ctx, "request_id", "req_01HZX4T7W8Y3N2M1K0J9Z8X7V6")
	hc.Add(ctx, "user_id", "usr_77451")
	hc.Add(ctx, "cache.hit", true)
	for i := 12; i < n; i++ {
		hc.Add(ctx, "k"+strconv.Itoa(i), i)
	}
}

// BenchmarkOperationLifecycle is the §4 kept-lifecycle gate
// (≤ 250 ns / ≤ 4 allocs), measured on the same corpus as the v0.4.0
// baseline: full OperationStart metadata plus one multi-pair Add.
func BenchmarkOperationLifecycle(b *testing.B) {
	rt := runtimeFor(discardSink{})
	b.ReportAllocs()
	for b.Loop() {
		var err error
		op := hc.Start(context.Background(), rt, hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "cleanup",
			ID:          "job_8472",
			Source:      "nightly",
			Attempt:     1,
			MaxAttempts: 3,
		})
		hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		op.End(&err)
	}
}

// BenchmarkOperationLifecycle12Fields is the same kept lifecycle with
// the 12-field medium corpus (the sink-axis event shape).
func BenchmarkOperationLifecycle12Fields(b *testing.B) {
	rt := runtimeFor(discardSink{})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
		benchmarkFields(op.Context(), 12)
		op.End(nil)
	}
}

// BenchmarkOperationLifecycleSparse is the kept lifecycle with no user
// fields — the floor of the core machinery.
func BenchmarkOperationLifecycleSparse(b *testing.B) {
	rt := runtimeFor(discardSink{})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{})
		op.End(nil)
	}
}

// BenchmarkOperationLifecycleDropped is the §4 full-dropped-lifecycle
// gate (≤ 300 ns), on the v0-parity corpus: N fields then a sampled-out
// End. "End-drop path ≤ 100 ns / ≤ 2 al" is the no-fields variant below.
func BenchmarkOperationLifecycleDropped(b *testing.B) {
	rt := hc.MustCompile(hc.Config{Sink: discardSink{}, SamplingRate: 0})
	for _, count := range []int{8, 32} {
		b.Run(strconv.Itoa(count)+"_fields", func(b *testing.B) {
			keys := make([]string, count)
			for i := range keys {
				keys[i] = "k" + strconv.Itoa(i)
			}
			b.ReportAllocs()
			for b.Loop() {
				op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "cleanup"})
				for _, k := range keys {
					hc.Add(op.Context(), k, 7)
				}
				op.End(nil)
			}
		})
	}
}

// BenchmarkOperationLifecycleDroppedNoFields approaches the end-drop
// floor (≤ 100 ns): sampler + release + pool with no field appends.
func BenchmarkOperationLifecycleDroppedNoFields(b *testing.B) {
	rt := hc.MustCompile(hc.Config{Sink: discardSink{}, Sampler: func(hc.SampleInput) bool { return false }})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{})
		op.End(nil)
	}
}

// BenchmarkOperationLifecycleWithPolicies runs the kept lifecycle under
// a policy table, as the v0 bench did.
func BenchmarkOperationLifecycleWithPolicies(b *testing.B) {
	policies := map[hc.Domain]hc.OperationPolicy{
		hc.DomainHTTP: {SuccessLevel: hc.LevelInfo, FailureLevel: hc.LevelError},
		hc.DomainJob:  {SuccessLevel: hc.LevelDebug, FailureLevel: hc.LevelWarn},
	}
	rt := hc.MustCompile(hc.Config{Sink: discardSink{}, SamplingRate: 1, OperationPolicies: policies})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "job"})
		benchmarkFields(op.Context(), 12)
		op.End(nil)
	}
}

// BenchmarkOperationPolicyScale runs the kept lifecycle with a wide
// policy table (the scale axis of the v0 quality matrix).
func BenchmarkOperationPolicyScale(b *testing.B) {
	policies := make(map[hc.Domain]hc.OperationPolicy, 128)
	for i := 0; i < 128; i++ {
		policies[hc.Domain("svc"+strconv.Itoa(i))] = hc.OperationPolicy{SuccessLevel: hc.LevelInfo}
	}
	rt := hc.MustCompile(hc.Config{Sink: discardSink{}, SamplingRate: 1, OperationPolicies: policies})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /x"})
		op.End(nil)
	}
}

// BenchmarkNonHTTPManualLifecycle is the background-job shape with an
// error result: explicit domain/name/id plus a few fields.
func BenchmarkNonHTTPManualLifecycle(b *testing.B) {
	rt := runtimeFor(discardSink{})
	b.ReportAllocs()
	for b.Loop() {
		var err error = errBench
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "import", ID: "j-1", Attempt: 1})
		hc.Add(op.Context(), "rows", 42)
		op.End(&err)
	}
}

var errBench = &benchError{}

type benchError struct{}

func (*benchError) Error() string { return "bench failure" }

// BenchmarkNonHTTPBackgroundJob is the success job shape.
func BenchmarkNonHTTPBackgroundJob(b *testing.B) {
	rt := runtimeFor(discardSink{})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "digest", ID: "d-9"})
		hc.Add(op.Context(), "batch", 100, "source", "queue")
		op.End(nil)
	}
}

// BenchmarkRateSampler measures the built-in probabilistic sampler.
func BenchmarkRateSampler(b *testing.B) {
	in := hc.SampleInput{Domain: hc.DomainHTTP, Operation: "GET /x", Outcome: hc.OutcomeSuccess, StatusCode: 200, Level: hc.LevelInfo}
	sampler := hc.RateSampler(0.5)
	b.ReportAllocs()
	for b.Loop() {
		_ = sampler(in)
	}
}

// BenchmarkDisabledLevelWrite measures the level-sampling drop for
// always-sampled-out debug traffic (no regression axis).
func BenchmarkDisabledLevelWrite(b *testing.B) {
	rt := hc.MustCompile(hc.Config{Sink: discardSink{}, SamplingRate: 0})
	b.ReportAllocs()
	for b.Loop() {
		op := hc.Start(context.Background(), rt, hc.OperationStart{})
		hc.SetLevel(op.Context(), hc.LevelDebug)
		op.End(nil)
	}
}
