package zapadapter

import (
	"testing"

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
	if entry.Message != "request_completed" {
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
