package benches_test

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

type nopResponseWriter struct{}

func (nopResponseWriter) Header() http.Header         { return nopHeader }
func (nopResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopResponseWriter) WriteHeader(int)             {}

var nopHeader = http.Header{}

func TestStressHeapStable(t *testing.T) {
	cfg := stressPolicyConfig(discardSink{})

	// Warm up pools/maps so their one-time growth does not count as a leak.
	var err error
	for range 100_000 {
		op := hc.StartOperation(context.Background(), hc.OperationStart{Domain: hc.DomainJob, Name: "warmup"})
		op.End(cfg, &err)
	}

	runtime.GC()
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	const ops = 2_000_000
	for range ops {
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

func TestStressConcurrentMixedAccess(t *testing.T) {
	cfg := stressPolicyConfig(discardSink{})
	sharedCtx, _ := hc.NewContext(context.Background())

	const goroutines = 32
	const iterations = 20_000

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range iterations {
				hc.Add(sharedCtx, "g", g, "i", i, "note", "x")
				hc.SetLevel(sharedCtx, hc.LevelWarn)
				hc.SetMessage(sharedCtx, "mixed")
				hc.Error(sharedCtx, errors.New("boom"))
				hc.EventFields(hc.FromContext(sharedCtx))

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

func TestStressSamplerUnderContention(t *testing.T) {
	tests := []struct {
		name string
		rate float64
	}{
		{name: "low", rate: 0.01},
		{name: "middle", rate: 0.5},
		{name: "high", rate: 0.99},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sampler := hc.RateSampler(tc.rate)

			const goroutines = 16
			const iterations = 100_000

			var kept atomic.Int64
			var wg sync.WaitGroup
			for range goroutines {
				wg.Go(func() {
					local := int64(0)
					for range iterations {
						if sampler(hc.SampleInput{Outcome: hc.OutcomeSuccess}) {
							local++
						}
					}
					kept.Add(local)
				})
			}
			wg.Wait()

			total := int64(goroutines * iterations)
			ratio := float64(kept.Load()) / float64(total)
			if ratio < tc.rate-0.005 || ratio > tc.rate+0.005 {
				t.Fatalf("sampled ratio = %.4f, want %.2f ± 0.005", ratio, tc.rate)
			}
			t.Logf("kept %d/%d = %.4f", kept.Load(), total, ratio)
		})
	}
}
