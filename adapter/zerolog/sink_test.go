package zerologadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	"github.com/rs/zerolog"
)

func TestSinkWriteMapsLevelAndFields(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	sink := New(&logger)

	sink.Write("WARN", "", map[string]any{
		"http.status": 429,
		"retry":       true,
	})

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode zerolog payload: %v", err)
	}
	if payload["level"] != "warn" {
		t.Fatalf("expected warn level, got %v", payload["level"])
	}
	if payload["message"] != "request_completed" {
		t.Fatalf("expected default message, got %v", payload["message"])
	}
	if payload["http.status"] != float64(429) {
		t.Fatalf("expected status field, got %v", payload["http.status"])
	}
	if payload["retry"] != true {
		t.Fatalf("expected retry field, got %v", payload["retry"])
	}
}

func TestSinkWriteAllSupportedFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	sink := New(&logger)
	now := time.Now().UTC().Truncate(time.Second)

	sink.Write(hc.LevelInfo, "typed", map[string]any{
		"s":   "x",
		"i":   int(1),
		"i8":  int8(2),
		"i16": int16(3),
		"i32": int32(4),
		"i64": int64(5),
		"u":   uint(6),
		"u8":  uint8(7),
		"u16": uint16(8),
		"u32": uint32(9),
		"u64": uint64(10),
		"f32": float32(1.5),
		"f64": float64(2.5),
		"b":   true,
		"t":   now,
		"d":   3 * time.Second,
		"e":   errors.New("boom"),
		"x":   map[string]any{"k": "v"},
	})

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode zerolog payload: %v", err)
	}

	for _, key := range []string{"s", "i", "i8", "i16", "i32", "i64", "u", "u8", "u16", "u32", "u64", "f32", "f64", "b", "t", "d", "e", "x"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload", key)
		}
	}
	if payload["e"] != "boom" {
		t.Fatalf("expected error string value, got %v", payload["e"])
	}
}

func TestSinkWriteMapsAllLevelsAndMessageBehavior(t *testing.T) {
	tests := []struct {
		name        string
		level       hc.Level
		message     string
		wantLevel   string
		wantMessage string
	}{
		{name: "debug", level: hc.LevelDebug, message: "m", wantLevel: "debug", wantMessage: "m"},
		{name: "warn", level: hc.LevelWarn, message: "m", wantLevel: "warn", wantMessage: "m"},
		{name: "error", level: hc.LevelError, message: "m", wantLevel: "error", wantMessage: "m"},
		{name: "default", level: "UNKNOWN", message: "", wantLevel: "info", wantMessage: "request_completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := zerolog.New(&buf)
			sink := New(&logger)
			sink.Write(tt.level, tt.message, map[string]any{"k": "v"})

			var payload map[string]any
			if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
				t.Fatalf("failed to decode zerolog payload: %v", err)
			}
			if payload["level"] != tt.wantLevel {
				t.Fatalf("level = %v, want %v", payload["level"], tt.wantLevel)
			}
			if payload["message"] != tt.wantMessage {
				t.Fatalf("message = %v, want %v", payload["message"], tt.wantMessage)
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
