package bench_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	hc "github.com/happytoolin/happycontext"
	stdhc "github.com/happytoolin/happycontext/integration/std"
)

func stressPolicyConfig(sink hc.Sink) hc.Config {
	return hc.NormalizeConfig(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		LevelSamplingRates: map[hc.Level]float64{
			hc.LevelDebug: 0.1,
			hc.LevelInfo:  1,
			hc.LevelWarn:  1,
			hc.LevelError: 1,
		},
		OperationPolicies: map[hc.Domain]hc.OperationPolicy{
			hc.DomainJob: {
				SuccessLevel: hc.LevelInfo,
				FailureLevel: hc.LevelError,
				PanicLevel:   hc.LevelError,
				OutcomeLevels: map[hc.Outcome]hc.Level{
					hc.OutcomeTimeout: hc.LevelWarn,
				},
			},
			hc.DomainHTTP: {
				SuccessLevel: hc.LevelInfo,
				FailureLevel: hc.LevelError,
				PanicLevel:   hc.LevelError,
			},
		},
	})
}

// BenchmarkStressParallelLifecycle runs full operation lifecycles across all
// cores with a policy-bearing config, the hottest per-request path.
func BenchmarkStressParallelLifecycle(b *testing.B) {
	cfg := stressPolicyConfig(discardSink{})
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var err error
		for pb.Next() {
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:  hc.DomainJob,
				Name:    "cleanup",
				ID:      "job_1",
				Attempt: 1,
			})
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
			op.End(cfg, &err)
		}
	})
}

// BenchmarkStressSustainedLifecycle measures single-goroutine sustained
// throughput; run with a long -benchtime for soak-style measurement.
func BenchmarkStressSustainedLifecycle(b *testing.B) {
	cfg := stressPolicyConfig(discardSink{})
	b.ReportAllocs()
	var err error
	for b.Loop() {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:  hc.DomainJob,
			Name:    "cleanup",
			ID:      "job_1",
			Attempt: 1,
		})
		hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		op.End(cfg, &err)
	}
}

// BenchmarkStressParallelStdMiddleware drives the std HTTP middleware (no
// network) from all cores.
func BenchmarkStressParallelStdMiddleware(b *testing.B) {
	cfg := stressPolicyConfig(discardSink{})
	handler := stdhc.Middleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		rw := nopResponseWriter{}
		for pb.Next() {
			handler.ServeHTTP(rw, req)
		}
	})
}

// nopResponseWriter is a minimal http.ResponseWriter; the middleware only
// records the status code and never touches the header map in this path.
type nopResponseWriter struct{}

func (nopResponseWriter) Header() http.Header       { return nopHeader }
func (nopResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopResponseWriter) WriteHeader(int)             {}

var nopHeader = http.Header{}

// TestStressHeapStable runs millions of operation lifecycles and verifies the
// heap returns to its baseline size afterwards: nothing per-event may be
// retained.
func TestStressHeapStable(t *testing.T) {
	cfg := stressPolicyConfig(discardSink{})

	// Warm up pools/maps so their one-time growth does not count as a leak.
	var err error
	for i := 0; i < 100_000; i++ {
		op := hc.StartOperation(context.Background(), hc.OperationStart{Domain: hc.DomainJob, Name: "warmup"})
		op.End(cfg, &err)
	}

	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const ops = 2_000_000
	for i := 0; i < ops; i++ {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:  hc.DomainJob,
			Name:    "cleanup",
			ID:      "job_1",
			Attempt: 1,
		})
		hc.Add(op.Context(), "worker", "payments")
		op.End(cfg, &err)
	}

	runtime.GC()
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	grew := int64(after.HeapInuse) - int64(before.HeapInuse)
	if grew > 32<<20 {
		t.Fatalf("heap grew by %d bytes after %d ops; suspected retention", grew, ops)
	}
	t.Logf("heap growth after %d ops: %d bytes (%.2f bytes/op)", ops, grew, float64(grew)/ops)
}

// TestStressConcurrentMixedAccess hammers the event API from many goroutines:
// a shared event for the Add/Set* storm, and per-goroutine operation finishes
// sharing one config (the sharing fast path must be race-free read-only).
// Run with -race.
func TestStressConcurrentMixedAccess(t *testing.T) {
	cfg := stressPolicyConfig(discardSink{})
	sharedCtx, _ := hc.NewContext(context.Background())

	const goroutines = 32
	const iterations = 20_000

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Storm the shared event.
				hc.Add(sharedCtx, "g", g, "i", i, "note", "x")
				hc.SetLevel(sharedCtx, hc.LevelWarn)
				hc.SetMessage(sharedCtx, "mixed")
				hc.Error(sharedCtx, errors.New("boom"))
				hc.EventFields(hc.FromContext(sharedCtx))

				// Independent lifecycle against the shared config.
				var err error
				op := hc.StartOperation(context.Background(), hc.OperationStart{
					Domain: hc.DomainJob, Name: "stress", ID: "id", Attempt: 1,
				})
				hc.Add(op.Context(), "worker", "payments")
				op.End(cfg, &err)
			}
		}(g)
	}
	wg.Wait()
}

// TestStressSamplerUnderContention verifies RateSampler stays statistically
// sound while all cores hammer the shared PRNG state.
func TestStressSamplerUnderContention(t *testing.T) {
	sampler := hc.RateSampler(0.5)

	const goroutines = 16
	const iterations = 100_000

	var kept atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := int64(0)
			for i := 0; i < iterations; i++ {
				if sampler(hc.SampleInput{Outcome: hc.OutcomeSuccess}) {
					local++
				}
			}
			kept.Add(local)
		}()
	}
	wg.Wait()

	total := int64(goroutines * iterations)
	ratio := float64(kept.Load()) / float64(total)
	if ratio < 0.45 || ratio > 0.55 {
		t.Fatalf("sampled ratio = %.4f, want within [0.45, 0.55]", ratio)
	}
	t.Logf("kept %d/%d = %.4f", kept.Load(), total, ratio)
}
