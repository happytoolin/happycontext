package zapadapter

import (
	"io"
	"strconv"
	"testing"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

func BenchmarkAdapter_zap(b *testing.B) {
	encoderCfg := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(io.Discard),
		zapcore.InfoLevel,
	)
	sink := New(zap.New(core))
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
}
