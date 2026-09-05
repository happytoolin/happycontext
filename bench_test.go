package hc

import (
	"context"
	"io"
	"testing"
	"time"
)

// These benches measure the gate segments that the external benches
// cannot isolate (they run inside package hc with unexported access).

// BenchmarkEndDropPath is the §4 end-drop gate segment: ≤ 100 ns / ≤ 2
// allocs for sampler + release + pool, excluding Start and field
// appends — operations are pre-built outside the timed loop.
func BenchmarkEndDropPath(b *testing.B) {
	rt := MustCompile(Config{Sink: dropCountSink{}, SamplingRate: 0})
	const pre = 4096
	ops := make([]*Operation, pre)
	rebuild := func() {
		for i := range ops {
			ops[i] = Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "cleanup"})
		}
	}
	rebuild()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(pre-1) == 0 && n > 0 {
			b.StopTimer()
			rebuild()
			b.StartTimer()
		}
		ops[n&(pre-1)].End(nil)
	}
}

// BenchmarkEndDropPathCustomSampler is the same segment with a custom
// sampler (the amendment-4 path: one closure call before the drop).
func BenchmarkEndDropPathCustomSampler(b *testing.B) {
	rt := MustCompile(Config{Sink: dropCountSink{}, Sampler: func(SampleInput) bool { return false }})
	const pre = 4096
	ops := make([]*Operation, pre)
	rebuild := func() {
		for i := range ops {
			ops[i] = Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "cleanup"})
		}
	}
	rebuild()
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if n&(pre-1) == 0 && n > 0 {
			b.StopTimer()
			rebuild()
			b.StartTimer()
		}
		ops[n&(pre-1)].End(nil)
	}
}

type dropCountSink struct{}

func (dropCountSink) Write(context.Context, *Record) {}

// BenchmarkRecordEncodeWrite12 is the fresh-record sink gate shape:
// encode a 12-field record once and write it (the lifecycle itself is
// benched separately in benches).
func BenchmarkRecordEncodeWrite12(b *testing.B) {
	fields := make([]Field, 12)
	keys := []string{"http.method", "http.path", "http.route", "http.status", "op.domain", "op.name", "op.outcome", "op.code", "duration_ms", "request_id", "user_id", "cache.hit"}
	for i, k := range keys {
		fields[i] = fieldStr(k, "v")
	}
	completedAt := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		rec := &Record{level: LevelInfo, msg: DefaultMessage, fields: fields, completedAt: completedAt}
		_, _ = io.Discard.Write(rec.Encoded())
	}
}
