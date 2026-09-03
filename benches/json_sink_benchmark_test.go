package benches_test

import (
	"context"
	"io"
	"strings"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

// captureRecords drives n kept lifecycles with fieldsFor(n) fields and
// returns the records a sink saw (one per request).
type recordCapture struct {
	recs []*hc.Record
}

func (c *recordCapture) Write(_ context.Context, rec *hc.Record) {
	c.recs = append(c.recs, rec)
}

func captureRecords(n int, fields int) []*hc.Record {
	cap := &recordCapture{}
	rt := hc.MustCompile(hc.Config{Sink: cap, SamplingRate: 1})
	for i := 0; i < n; i++ {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
		benchmarkFields(op.Context(), fields)
		op.End(nil)
	}
	return cap.recs
}

// BenchmarkJSONSink measures the sink write path with pre-encoded
// records (Encoded() cached after the first call — the loop measures
// Write of the canonical line).
func BenchmarkJSONSink(b *testing.B) {
	sink := hc.NewJSONSink(io.Discard)
	recs := captureRecords(64, 12)
	b.Run("write_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			sink.Write(context.Background(), recs[i&63])
			i++
		}
	})
}

// BenchmarkJSONSinkLifecycle is the honest end-to-end sink gate:
// Start + 12 fields + End through NewJSONSink (encode once + single
// Write). The §4 sink gate (≤ 400 ns / ≤ 2 allocs) was stated for the
// v0.6 sink-write shape; this is the v2 lifecycle inclusive of it.
func BenchmarkJSONSinkLifecycle(b *testing.B) {
	rt := hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(io.Discard), SamplingRate: 1})
	b.Run("12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
			benchmarkFields(op.Context(), 12)
			op.End(nil)
		}
	})
	b.Run("0_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			op := hc.Start(context.Background(), rt, hc.OperationStart{})
			op.End(nil)
		}
	})
}

// BenchmarkJSONSinkEscaping isolates escape-scan cost at the sink level.
func BenchmarkJSONSinkEscaping(b *testing.B) {
	rt := hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(io.Discard), SamplingRate: 1})
	b.Run("escape_heavy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			op := hc.Start(context.Background(), rt, hc.OperationStart{})
			ctx := op.Context()
			hc.Add(ctx, "url", "/search?q="+strings.Repeat("héllo☃", 8))
			hc.Add(ctx, "agent", `Mozilla/5.0 (X11; "quote" back\slash) Engine/1.0`)
			op.End(nil)
		}
	})
}
