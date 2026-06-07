package zerologadapter

import (
	"io"
	"strconv"
	"testing"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
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
	for i := range 15 {
		m["k"+strconv.Itoa(i)] = i
	}
	m["http.status"] = 200
	m["feature"] = "checkout"
	return m
}

func BenchmarkAdapter_zerolog(b *testing.B) {
	logger := zerolog.New(io.Discard)
	sink := New(&logger)
	medium := benchFieldsMedium()
	smallFields := fieldListFromMap(benchFieldsSmall)
	mediumFields := fieldListFromMap(medium)
	start := hc.OperationStart{Domain: hc.DomainJob, Name: "cleanup", ID: "job_8472", Source: "nightly", Attempt: 1, MaxAttempts: 3}

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

	b.Run("write_fields_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFields(hc.LevelInfo, "request_completed", smallFields)
		}
	})

	b.Run("write_fields_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFields(hc.LevelInfo, "request_completed", mediumFields)
		}
	})

	b.Run("write_fields_completion_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFieldsWithCompletion(hc.LevelInfo, "request_completed", smallFields, 7, 204, hc.OutcomeSuccess)
		}
	})

	b.Run("write_fields_completion_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFieldsWithCompletion(hc.LevelInfo, "request_completed", mediumFields, 7, 200, hc.OutcomeSuccess)
		}
	})

	b.Run("write_fields_operation_completion_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFieldsWithOperationCompletion(hc.LevelInfo, "operation_completed", smallFields, start, 7, 0, hc.OutcomeSuccess)
		}
	})

	b.Run("write_fields_operation_completion_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.WriteFieldsWithOperationCompletion(hc.LevelInfo, "operation_completed", mediumFields, start, 7, 0, hc.OutcomeSuccess)
		}
	})

}

func fieldListFromMap(fields map[string]any) []hc.Field {
	list := make([]hc.Field, 0, len(fields))
	for key, value := range fields {
		list = append(list, hc.Field{Key: key, Value: value})
	}
	return list
}
