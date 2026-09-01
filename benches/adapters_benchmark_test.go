package benches_test

import (
	"context"
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

// adapterEvent runs one 12-field lifecycle through the sink under test.
func adapterEvent(ctx context.Context, rt *hc.Runtime) {
	op := hc.Start(ctx, rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
	benchmarkFields(op.Context(), 12)
	op.End(nil)
}

// bridgeRecords pre-builds records via a capturing sink (the bridge-only
// gate shape: sink.Write on a ready record, mirroring the v0 benches).
type bridgeCapture struct{ recs []*hc.Record }

func (c *bridgeCapture) Write(_ context.Context, rec *hc.Record) { c.recs = append(c.recs, rec) }

func bridgeRecords(n int) []*hc.Record {
	cap := &bridgeCapture{}
	rt := hc.MustCompile(hc.Config{Sink: cap, SamplingRate: 1})
	for i := 0; i < 64; i++ {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainHTTP, Name: "GET /api/v1/orders/:id"})
		benchmarkFields(op.Context(), n)
		op.End(nil)
	}
	return cap.recs
}

func BenchmarkHostFloors(b *testing.B) {
	b.Run("slog_json_12_fields", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		ctx := context.Background()
		b.ReportAllocs()
		for b.Loop() {
			logger.LogAttrs(ctx, slog.LevelInfo, "request_completed",
				slog.String("http.method", "GET"),
				slog.String("http.path", "/api/v1/orders/12345"),
				slog.String("http.route", "/api/v1/orders/:id"),
				slog.Int64("http.status", 200),
				slog.String("op.domain", "http"),
				slog.String("op.name", "GET /api/v1/orders/:id"),
				slog.String("op.outcome", "success"),
				slog.Int64("op.code", 200),
				slog.Int64("duration_ms", 12),
				slog.String("request_id", "req_01HZX4T7W8Y3N2M1K0J9Z8X7V6"),
				slog.String("user_id", "usr_77451"),
				slog.Bool("cache.hit", true))
		}
	})
	b.Run("zap_json_12_fields", func(b *testing.B) {
		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(io.Discard),
			zapcore.DebugLevel,
		)
		logger := zap.New(core)
		b.ReportAllocs()
		for b.Loop() {
			logger.Info("request_completed",
				zap.String("http.method", "GET"),
				zap.String("http.path", "/api/v1/orders/12345"),
				zap.String("http.route", "/api/v1/orders/:id"),
				zap.Int64("http.status", 200),
				zap.String("op.domain", "http"),
				zap.String("op.name", "GET /api/v1/orders/:id"),
				zap.String("op.outcome", "success"),
				zap.Int64("op.code", 200),
				zap.Int64("duration_ms", 12),
				zap.String("request_id", "req_01HZX4T7W8Y3N2M1K0J9Z8X7V6"),
				zap.String("user_id", "usr_77451"),
				zap.Bool("cache.hit", true))
		}
	})
	b.Run("zerolog_12_fields", func(b *testing.B) {
		logger := zerolog.New(io.Discard)
		b.ReportAllocs()
		for b.Loop() {
			logger.Info().
				Str("http.method", "GET").
				Str("http.path", "/api/v1/orders/12345").
				Str("http.route", "/api/v1/orders/:id").
				Int64("http.status", 200).
				Str("op.domain", "http").
				Str("op.name", "GET /api/v1/orders/:id").
				Str("op.outcome", "success").
				Int64("op.code", 200).
				Int64("duration_ms", 12).
				Str("request_id", "req_01HZX4T7W8Y3N2M1K0J9Z8X7V6").
				Str("user_id", "usr_77451").
				Bool("cache.hit", true).
				Msg("request_completed")
		}
	})
}

func BenchmarkAdapterSlog(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sink := slogadapter.New(logger)
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	ctx := context.Background()
	recs := bridgeRecords(12)

	b.Run("bridge_only_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			sink.Write(ctx, recs[i&63])
			i++
		}
	})

	b.Run("write_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			adapterEvent(ctx, rt)
		}
	})
	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hc.Start(ctx, rt, hc.OperationStart{}).End(nil)
		}
	})
}

func BenchmarkAdapterZap(b *testing.B) {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(io.Discard),
		zapcore.DebugLevel,
	)
	sink := zapadapter.New(zap.New(core))
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	ctx := context.Background()
	recs := bridgeRecords(12)

	b.Run("bridge_only_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			sink.Write(ctx, recs[i&63])
			i++
		}
	})

	b.Run("write_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			adapterEvent(ctx, rt)
		}
	})
	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hc.Start(ctx, rt, hc.OperationStart{}).End(nil)
		}
	})
}

func BenchmarkAdapterZerolog(b *testing.B) {
	logger := zerolog.New(io.Discard)
	sink := zerologadapter.New(&logger)
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	ctx := context.Background()
	recs := bridgeRecords(12)

	b.Run("bridge_only_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		i := 0
		for b.Loop() {
			sink.Write(ctx, recs[i&63])
			i++
		}
	})

	b.Run("write_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			adapterEvent(ctx, rt)
		}
	})
	b.Run("write_empty", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			hc.Start(ctx, rt, hc.OperationStart{}).End(nil)
		}
	})
}
