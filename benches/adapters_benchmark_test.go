package benches_test

import (
	"io"
	"log/slog"
	"testing"

	hc "github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	zapadapter "github.com/happytoolin/happycontext/adapter/zap"
	zerologadapter "github.com/happytoolin/happycontext/adapter/zerolog"
	"github.com/rs/zerolog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var adapterFieldsSmall = map[string]any{
	"http.method": "GET",
	"http.path":   "/orders/123",
	"http.status": 204,
	"duration_ms": 7,
	"user_id":     "u_1",
	"plan":        "pro",
}

func adapterFieldsMedium() map[string]any {
	fields := buildBenchmarkFields(15)
	fields["http.status"] = 200
	fields["feature"] = "checkout"
	return fields
}

func BenchmarkAdapterSlog(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sink := slogadapter.New(logger)
	medium := adapterFieldsMedium()

	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", nil)
		}
	})
	b.Run("write_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", adapterFieldsSmall)
		}
	})
	b.Run("write_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
	b.Run("write_disabled_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelDebug, "request_completed", medium)
		}
	})
}

func BenchmarkAdapterZap(b *testing.B) {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(io.Discard),
		zapcore.InfoLevel,
	)
	sink := zapadapter.New(zap.New(core))
	medium := adapterFieldsMedium()

	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", nil)
		}
	})
	b.Run("write_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", adapterFieldsSmall)
		}
	})
	b.Run("write_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
	b.Run("write_disabled_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelDebug, "request_completed", medium)
		}
	})
}

func BenchmarkAdapterZerolog(b *testing.B) {
	logger := zerolog.New(io.Discard)
	sink := zerologadapter.New(&logger)
	disabledLogger := logger.Level(zerolog.InfoLevel)
	disabledSink := zerologadapter.New(&disabledLogger)
	medium := adapterFieldsMedium()

	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", nil)
		}
	})
	b.Run("write_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", adapterFieldsSmall)
		}
	})
	b.Run("write_warn_small", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelWarn, "request_completed", adapterFieldsSmall)
		}
	})
	b.Run("write_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
	b.Run("write_disabled_medium", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			disabledSink.Write(hc.LevelDebug, "request_completed", medium)
		}
	})
}

func BenchmarkAdapterSlogParallel(b *testing.B) {
	sink := slogadapter.New(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	fields := adapterFieldsMedium()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sink.Write(hc.LevelInfo, "request_completed", fields)
		}
	})
}
