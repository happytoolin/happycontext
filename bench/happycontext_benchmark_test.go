package bench_test

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
)

func BenchmarkEventAddStableKeys(b *testing.B) {
	ctx, _ := hc.NewContext(context.Background())
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		hc.Add(ctx, keys[i&31], i)
		i++
	}
}

func BenchmarkEventAddMany(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := hc.NewContext(context.Background())
		hc.Add(
			ctx,
			"user_id", "u_8472",
			"cart_items", 3,
			"cart_total", 300,
			"country", "US",
			"feature_flag", true,
		)
	}
}

func BenchmarkEventAddFieldsMany(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := hc.NewContext(context.Background())
		hc.AddFields(
			ctx,
			hc.String("user_id", "u_8472"),
			hc.Int("cart_items", 3),
			hc.Int("cart_total", 300),
			hc.String("country", "US"),
			hc.Bool("feature_flag", true),
		)
	}
}

func BenchmarkEventAddParallelStableKeys(b *testing.B) {
	ctx, _ := hc.NewContext(context.Background())
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			hc.Add(ctx, keys[i&31], i)
			i++
		}
	})
}

func BenchmarkEventSnapshot(b *testing.B) {
	for _, n := range []int{8, 32, 128} {
		b.Run("fields_"+strconv.Itoa(n), func(b *testing.B) {
			ctx, _ := hc.NewContext(context.Background())
			for i := range n {
				hc.Add(ctx, "k"+strconv.Itoa(i), i)
			}

			b.ReportAllocs()
			for b.Loop() {
				_ = hc.EventFields(hc.FromContext(ctx))
			}
		})
	}
}

func BenchmarkEventSnapshotNested(b *testing.B) {
	ctx, _ := hc.NewContext(context.Background())
	hc.Add(ctx, "request", map[string]any{
		"user": map[string]any{
			"id":    "u_1",
			"roles": []any{"admin", "billing"},
		},
		"flags": []any{
			map[string]any{"name": "beta", "enabled": true},
			map[string]any{"name": "new_pricing", "enabled": false},
		},
	})

	b.ReportAllocs()
	for b.Loop() {
		_ = hc.EventFields(hc.FromContext(ctx))
	}
}

func BenchmarkEventSnapshotCyclic(b *testing.B) {
	ctx, _ := hc.NewContext(context.Background())
	node := map[string]any{"name": "root"}
	node["self"] = node
	hc.Add(ctx, "node", node)

	b.ReportAllocs()
	for b.Loop() {
		_ = hc.EventFields(hc.FromContext(ctx))
	}
}

func BenchmarkCommitPath(b *testing.B) {
	sink := discardSink{}

	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := hc.NewContext(context.Background())
		hc.Add(
			ctx,
			"http.method", "GET",
			"http.path", "/checkout",
			"http.status", 200,
			"duration_ms", 12,
			"user_id", "u_8472",
			"user_plan", "premium",
			"db.query_count", 3,
		)
		sink.Write(hc.LevelInfo, "request_completed", hc.EventFields(hc.FromContext(ctx)))
	}
}

type discardSink struct{}

func (discardSink) Write(_ hc.Level, _ string, _ map[string]any) {}

type flatFields struct {
	key   string
	value any
	kv    []any
	ok    bool
}

func buildBenchmarkFields(count int) map[string]any {
	fields := make(map[string]any, count)
	for i := range count {
		fields["field_"+strconv.Itoa(i)] = i
	}
	return fields
}

func flattenFields(fields map[string]any) flatFields {
	if len(fields) == 0 {
		return flatFields{}
	}
	flat := flatFields{kv: make([]any, 0, (len(fields)-1)*2), ok: true}
	first := true
	for key, value := range fields {
		if first {
			flat.key = key
			flat.value = value
			first = false
			continue
		}
		flat.kv = append(flat.kv, key, value)
	}
	return flat
}

func BenchmarkJSONEncodingReference(b *testing.B) {
	payload := []byte(`{"status":"ok"}`)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = io.Discard.Write(payload)
	}
}

func BenchmarkNonHTTPManualLifecycle(b *testing.B) {
	sink := discardSink{}

	fieldProfiles := map[string]map[string]any{
		"small": {
			"job.type":    "cleanup",
			"job.id":      "job_1",
			"job.success": true,
			"duration_ms": int64(10),
			"retry":       false,
			"attempt":     1,
		},
		"medium": buildBenchmarkFields(15),
		"large":  buildBenchmarkFields(40),
	}

	for name, fields := range fieldProfiles {
		flat := flattenFields(fields)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ctx, _ := hc.NewContext(context.Background())
				if flat.ok {
					hc.Add(ctx, flat.key, flat.value, flat.kv...)
				}
				sink.Write(hc.LevelInfo, "job_completed", hc.EventFields(hc.FromContext(ctx)))
			}
		})
	}
}

func BenchmarkNonHTTPBackgroundJob(b *testing.B) {
	sink := discardSink{}

	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := hc.NewContext(context.Background())
		hc.Add(ctx, "job.id", "job_8472")
		hc.Add(ctx, "worker", "payments")
		hc.Add(ctx, "attempt", 1)
		hc.Add(ctx, "duration_ms", 13)
		hc.Add(ctx, "scheduled_at", time.Now().UTC().Truncate(time.Second))
		hc.Commit(ctx, sink, hc.LevelInfo)
	}
}

func BenchmarkOperationLifecycle(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueDirectAddFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueDirectAdd2FastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleLocalDirectAdd2FastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleInPlaceDirectAdd2FastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleInPlaceReuseDirectAdd2FastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleLocalDirectAdd2FinishFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "cleanup",
			ID:          "job_8472",
			Source:      "nightly",
			Attempt:     1,
			MaxAttempts: 3,
		})
		op.Add2("worker", "payments", "tenant", "enterprise")
		op.Finish(cfg, nil)
	}
}

func BenchmarkOperationLifecycleInPlaceReuseDirectAdd2FinishFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "cleanup",
			ID:          "job_8472",
			Source:      "nightly",
			Attempt:     1,
			MaxAttempts: 3,
		})
		op.Add2("worker", "payments", "tenant", "enterprise")
		op.Finish(cfg, nil)
	}
}

func BenchmarkOperationLifecycleValueDirectTypedFastSink(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2Strings("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleSampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueSampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			hc.Add(op.Context(), "worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueDirectTypedSampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2Strings("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleValueDirectAdd2SampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleLocalDirectAdd2SampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleInPlaceDirectAdd2SampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleInPlaceReuseDirectAdd2SampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		func() {
			var err error
			op := hc.StartOperation(context.Background(), hc.OperationStart{
				Domain:      hc.DomainJob,
				Name:        "cleanup",
				ID:          "job_8472",
				Source:      "nightly",
				Attempt:     1,
				MaxAttempts: 3,
			})
			defer op.End(cfg, &err)
			op.Add2("worker", "payments", "tenant", "enterprise")
		}()
	}
}

func BenchmarkOperationLifecycleLocalDirectAdd2FinishSampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "cleanup",
			ID:          "job_8472",
			Source:      "nightly",
			Attempt:     1,
			MaxAttempts: 3,
		})
		op.Add2("worker", "payments", "tenant", "enterprise")
		op.Finish(cfg, nil)
	}
}

func BenchmarkOperationLifecycleInPlaceReuseDirectAdd2FinishSampledOut(b *testing.B) {
	sink := discardSink{}
	cfg := hc.Config{Sink: sink, SamplingRate: 0}

	b.ReportAllocs()
	for b.Loop() {
		op := hc.StartOperation(context.Background(), hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        "cleanup",
			ID:          "job_8472",
			Source:      "nightly",
			Attempt:     1,
			MaxAttempts: 3,
		})
		op.Add2("worker", "payments", "tenant", "enterprise")
		op.Finish(cfg, nil)
	}
}
