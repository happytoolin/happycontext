package zapadapter

import (
	"sync"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSinkWriteMapsLevelAndMessage(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	sink := New(logger)

	sink.Write("ERROR", "", map[string]any{
		"http.status": 500,
		"user_id":     "u_1",
	})

	if logs.Len() != 1 {
		t.Fatalf("expected one log entry, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Level != zapcore.ErrorLevel {
		t.Fatalf("expected error level, got %v", entry.Level)
	}
	if entry.Message != hc.DefaultMessage {
		t.Fatalf("expected default message, got %q", entry.Message)
	}
	if got := entry.ContextMap()["http.status"]; got != int64(500) {
		t.Fatalf("expected status field, got %v", got)
	}
	if got := entry.ContextMap()["user_id"]; got != "u_1" {
		t.Fatalf("expected user_id field, got %v", got)
	}
}

func TestSinkWriteMapsAllKnownLevels(t *testing.T) {
	tests := []struct {
		name  string
		level hc.Level
		want  zapcore.Level
	}{
		{name: "debug", level: hc.LevelDebug, want: zapcore.DebugLevel},
		{name: "warn", level: hc.LevelWarn, want: zapcore.WarnLevel},
		{name: "error", level: hc.LevelError, want: zapcore.ErrorLevel},
		{name: "default", level: "UNKNOWN", want: zapcore.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			sink := New(zap.New(core))

			sink.Write(tt.level, "done", map[string]any{"k": "v"})

			if logs.Len() != 1 {
				t.Fatalf("expected one log entry, got %d", logs.Len())
			}
			entry := logs.All()[0]
			if entry.Level != tt.want {
				t.Fatalf("level = %v, want %v", entry.Level, tt.want)
			}
			if entry.Message != "done" {
				t.Fatalf("message = %q, want %q", entry.Message, "done")
			}
			if got := entry.ContextMap()["k"]; got != "v" {
				t.Fatalf("missing field, got %v", got)
			}
		})
	}
}

func TestSinkWriteNilSafety(t *testing.T) {
	var nilSink *Sink
	nilSink.Write(hc.LevelInfo, "x", map[string]any{"k": 1})

	sink := New(nil)
	sink.Write(hc.LevelInfo, "x", map[string]any{"k": 1})
}

func TestSinkCheckPreservesFilteringHooksAndSampling(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	hookCalls := 0
	logger := zap.New(core, zap.Hooks(func(zapcore.Entry) error {
		hookCalls++
		return nil
	}))
	sink := New(logger)

	sink.Write(hc.LevelDebug, "disabled", map[string]any{"k": "v"})
	if logs.Len() != 0 || hookCalls != 0 {
		t.Fatalf("disabled write: logs=%d hooks=%d", logs.Len(), hookCalls)
	}
	sink.Write(hc.LevelWarn, "enabled", map[string]any{"k": "v"})
	if logs.Len() != 1 || hookCalls != 1 {
		t.Fatalf("enabled write: logs=%d hooks=%d", logs.Len(), hookCalls)
	}

	sampledCore, sampledLogs := observer.New(zapcore.DebugLevel)
	sampled := zapcore.NewSamplerWithOptions(sampledCore, time.Hour, 1, 0)
	sampledSink := New(zap.New(sampled))
	sampledSink.Write(hc.LevelInfo, "same", nil)
	sampledSink.Write(hc.LevelInfo, "same", nil)
	if sampledLogs.Len() != 1 {
		t.Fatalf("sampled logs = %d, want 1", sampledLogs.Len())
	}
}

func TestSinkCheckPreservesCallerOptions(t *testing.T) {
	for _, skip := range []int{0, 1} {
		core, logs := observer.New(zapcore.DebugLevel)
		sink := New(zap.New(core, zap.AddCaller(), zap.AddCallerSkip(skip)))
		sink.Write(hc.LevelInfo, "caller", nil)
		if logs.Len() != 1 || !logs.All()[0].Caller.Defined {
			t.Fatalf("skip %d: caller = %+v", skip, logs.All())
		}
	}
}

func TestRecycleSliceClearsAndCaps(t *testing.T) {
	fields := make([]zap.Field, 0, zapPoolCapacity)
	for range 100 {
		fields = append(fields, zap.Field{})
	}
	if cap(fields) > zapPoolMaxCapacity {
		t.Fatalf("100-field buffer exceeds pool limit: cap=%d limit=%d", cap(fields), zapPoolMaxCapacity)
	}
	fields[0] = zap.Any("retained", new(int))
	var fieldPool sync.Pool
	recycleSlice(&fieldPool, &fields, fields)
	first := fields[:cap(fields)][0]
	if len(fields) != 0 || first.Key != "" || first.Interface != nil {
		t.Fatalf("field buffer was not cleared and recycled: len=%d first=%v", len(fields), first)
	}

	oversized := make([]zap.Field, zapPoolMaxCapacity+1)
	recycleSlice(&fieldPool, &oversized, oversized)
	if len(oversized) != zapPoolMaxCapacity+1 {
		t.Fatalf("oversized buffer was retained: len=%d", len(oversized))
	}
}
