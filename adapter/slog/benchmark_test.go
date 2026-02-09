package slogadapter

import (
	"io"
	"log/slog"
	"strconv"
	"testing"

	"github.com/happytoolin/happycontext"
)

var benchFieldsSmall = map[string]any{
	"http.method": "GET",
	"http.path":   "/orders/123",
	"http.status": 204,
	"duration_ms": 7,
	"user_id":     "u_1",
	"plan":        "pro",
}

func benchFieldsMedium() map[string]any {
	m := make(map[string]any, 15)
	for i := 0; i < 15; i++ {
		m["k"+strconv.Itoa(i)] = i
	}
	m["http.status"] = 200
	m["feature"] = "checkout"
	return m
}

func BenchmarkAdapter_slog(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sink := New(logger)
	sinkDeterministic := NewWithOptions(logger, SinkOptions{DeterministicOrder: true})
	medium := benchFieldsMedium()

	b.Run("write_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", benchFieldsSmall)
		}
	})

	b.Run("write_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", medium)
		}
	})

	b.Run("write_medium_deterministic", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkDeterministic.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
}
