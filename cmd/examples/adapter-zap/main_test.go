package main

import (
	"bytes"
	"context"
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

	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1, Message: "zap test message"})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "example", "adapter-zap", "test", true)
	op.End(nil)

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
	sink.Write(context.Background(), nil)
}

func TestZapAdapterAllLevels(t *testing.T) {
	levels := []hc.Level{hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError}

	for _, level := range levels {
		var buf bytes.Buffer
		encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
		core := zapcore.NewCore(encoder, zapcore.AddSync(&buf), zapcore.DebugLevel)
		logger := zap.New(core)
		sink := zaphc.New(logger)
		rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1, Message: "level test"})
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		hc.SetLevel(op.Context(), level)
		op.End(nil)

		if buf.Len() == 0 {
			t.Errorf("expected output for level %s", level)
		}
	}
}
