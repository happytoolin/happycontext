package bench_test

import (
	"context"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/happytoolin/hlog"
)

func BenchmarkEventAddStableKeys(b *testing.B) {
	e := hlog.NewEvent()
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		e.Add(keys[i&31], i)
		i++
	}
}

func BenchmarkEventAddMap(b *testing.B) {
	template := map[string]any{
		"user_id":      "u_8472",
		"cart_items":   3,
		"cart_total":   300,
		"country":      "US",
		"feature_flag": true,
	}

	b.ReportAllocs()
	for b.Loop() {
		e := hlog.NewEvent()
		e.AddMap(template)
	}
}

func BenchmarkEventAddParallelStableKeys(b *testing.B) {
	e := hlog.NewEvent()
	keys := make([]string, 32)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			e.Add(keys[i&31], i)
			i++
		}
	})
}

func BenchmarkEventSnapshot(b *testing.B) {
	for _, n := range []int{8, 32, 128} {
		b.Run("fields_"+strconv.Itoa(n), func(b *testing.B) {
			e := hlog.NewEvent()
			for i := 0; i < n; i++ {
				e.Add("k"+strconv.Itoa(i), i)
			}

			b.ReportAllocs()
			for b.Loop() {
				_ = e.Snapshot()
			}
		})
	}
}

func BenchmarkEventSnapshotNested(b *testing.B) {
	e := hlog.NewEvent()
	e.Add("request", map[string]any{
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
		_ = e.Snapshot()
	}
}

func BenchmarkEventSnapshotCyclic(b *testing.B) {
	e := hlog.NewEvent()
	node := map[string]any{"name": "root"}
	node["self"] = node
	e.Add("node", node)

	b.ReportAllocs()
	for b.Loop() {
		_ = e.Snapshot()
	}
}

func BenchmarkCommitPath(b *testing.B) {
	baseFields := map[string]any{
		"http.method":    "GET",
		"http.path":      "/checkout",
		"http.status":    200,
		"duration_ms":    12,
		"user_id":        "u_8472",
		"user_plan":      "premium",
		"db.query_count": 3,
	}

	sink := discardSink{}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		e := hlog.NewEvent()
		e.AddMap(baseFields)
		s := e.Snapshot()
		sink.Write(ctx, hlog.LevelInfo, "request_completed", s.Fields)
	}
}

type discardSink struct{}

func (discardSink) Write(_ context.Context, _, _ string, _ map[string]any) {}

func BenchmarkJSONEncodingReference(b *testing.B) {
	payload := []byte(`{"status":"ok"}`)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = io.Discard.Write(payload)
	}
}

func BenchmarkNonHTTPManualLifecycle(b *testing.B) {
	ctx := context.Background()
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
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				e := hlog.NewEvent()
				e.AddMap(fields)
				snapshot := e.Snapshot()
				sink.Write(ctx, hlog.LevelInfo, "job_completed", snapshot.Fields)
			}
		})
	}
}

func BenchmarkNonHTTPBackgroundJob(b *testing.B) {
	sink := discardSink{}

	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := hlog.NewContext(context.Background())
		hlog.Add(ctx, "job.id", "job_8472")
		hlog.Add(ctx, "worker", "payments")
		hlog.Add(ctx, "attempt", 1)
		hlog.Add(ctx, "duration_ms", 13)
		hlog.Add(ctx, "scheduled_at", time.Now().UTC().Truncate(time.Second))
		hlog.Commit(ctx, sink, hlog.LevelInfo)
	}
}

func buildBenchmarkFields(n int) map[string]any {
	fields := make(map[string]any, n)
	for i := 0; i < n; i++ {
		fields["k"+strconv.Itoa(i)] = i
	}
	return fields
}
