package benches_test

import (
	"io"
	"strings"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

// jsonSinkFields12 is the §4 gate's "12-field medium case": a realistic
// canonical request event with typed values, not synthetic filler.
var jsonSinkFields12 = map[string]any{
	"http.method": "GET",
	"http.path":   "/api/v1/orders/12345",
	"http.route":  "/api/v1/orders/:id",
	"http.status": 200,
	"op.domain":   "http",
	"op.name":     "GET /api/v1/orders/:id",
	"op.outcome":  "success",
	"op.code":     200,
	"duration_ms": 12,
	"request_id":  "req_01HZX4T7W8Y3N2M1K0J9Z8X7V6",
	"user_id":     "usr_77451",
	"cache.hit":   true,
}

func jsonSinkFields8() map[string]any {
	return map[string]any{
		"http.method": "POST",
		"http.path":   "/api/v1/users",
		"http.status": 201,
		"op.domain":   "http",
		"op.outcome":  "success",
		"duration_ms": 34,
		"request_id":  "req_8c1f0e22",
		"attempt":     1,
	}
}

func jsonSinkFields32() map[string]any {
	fields := buildBenchmarkFields(32)
	fields["http.status"] = 500
	fields["op.outcome"] = "failure"
	fields["error"] = map[string]any{"message": "db timeout", "type": "*pq.error"}
	return fields
}

// BenchmarkJSONSink measures the first-party sink end to end (encode +
// single Write to io.Discard). Gate (V2_DESIGN §4): the 12-field medium
// case ≤ 400 ns/op and ≤ 2 allocs/op.
func BenchmarkJSONSink(b *testing.B) {
	sink := hc.NewJSONSink(io.Discard)
	small := adapterFieldsSmall
	medium := jsonSinkFields12
	wide := jsonSinkFields32()
	eight := jsonSinkFields8()

	cases := []struct {
		name   string
		fields map[string]any
	}{
		{"write_empty", nil},
		{"write_small", small},
		{"write_8_fields", eight},
		{"write_12_fields", medium},
		{"write_32_fields", wide},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sink.Write(hc.LevelInfo, "request_completed", c.fields)
			}
		})
	}
}

// BenchmarkJSONSinkEscaping isolates escape-scan cost at the sink level
// with hostile message strings.
func BenchmarkJSONSinkEscaping(b *testing.B) {
	sink := hc.NewJSONSink(io.Discard)
	fields := map[string]any{
		"url":   "/search?q=" + strings.Repeat("héllo☃", 8),
		"agent": `Mozilla/5.0 (X11; "quote" back\slash) Engine/1.0`,
	}
	b.Run("escape_heavy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", fields)
		}
	})
	b.Run("clean_ascii", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", jsonSinkFields12)
		}
	})
}
