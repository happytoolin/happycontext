package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/happytoolin/happycontext"
	zaphc "github.com/happytoolin/happycontext/adapter/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestZapAdapterWritesStructuredLogs(t *testing.T) {
	var buf bytes.Buffer
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(&buf), zapcore.InfoLevel)
	logger := zap.New(core)

	sink := zaphc.New(logger)
	if sink == nil {
		t.Fatal("expected sink to be created")
	}

	fields := map[string]any{
		"example": "adapter-zap",
		"test":    true,
	}

	sink.Write(hc.LevelInfo, "zap test message", fields)

	output := buf.String()
	if !strings.Contains(output, "zap test message") {
		t.Error("expected log output to contain 'zap test message'")
	}
	if !strings.Contains(output, "adapter-zap") {
		t.Error("expected log output to contain 'adapter-zap'")
	}
}

func TestZapAdapterWithNilLogger(t *testing.T) {
	sink := zaphc.New(nil)
	if sink == nil {
		t.Fatal("expected sink to be created even with nil logger")
	}

	// Should not panic when writing with nil logger
	sink.Write(hc.LevelInfo, "test", map[string]any{"key": "value"})
}

func TestZapAdapterAllLevels(t *testing.T) {
	levels := []hc.Level{hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError}

	for _, level := range levels {
		var buf bytes.Buffer
		encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
		core := zapcore.NewCore(encoder, zapcore.AddSync(&buf), zapcore.DebugLevel)
		logger := zap.New(core)
		sink := zaphc.New(logger)

		sink.Write(level, "level test", map[string]any{"level": string(level)})

		output := buf.String()
		if output == "" {
			t.Errorf("expected output for level %s", level)
		}
	}
}
