package slogadapter

import (
	"context"
	"log/slog"
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestSinkWriteMapsLevelAndDefaultsMessage(t *testing.T) {
	h := &captureSlogHandler{}
	logger := slog.New(h)
	sink := New(logger)

	sink.Write("WARN", "", map[string]any{
		"user_id": "u_1",
	})

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}
	if h.records[0].Message != hc.DefaultMessage {
		t.Fatalf("expected default message, got %q", h.records[0].Message)
	}
	if h.records[0].Level != slog.LevelWarn {
		t.Fatalf("expected warn level, got %v", h.records[0].Level)
	}
	if h.records[0].Attrs["user_id"] != "u_1" {
		t.Fatalf("missing user_id attr")
	}
}

func TestSinkWriteMapsAllKnownLevels(t *testing.T) {
	tests := []struct {
		name  string
		level hc.Level
		want  slog.Level
	}{
		{name: "debug", level: hc.LevelDebug, want: slog.LevelDebug},
		{name: "warn", level: hc.LevelWarn, want: slog.LevelWarn},
		{name: "error", level: hc.LevelError, want: slog.LevelError},
		{name: "default", level: hc.Level("UNKNOWN"), want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &captureSlogHandler{}
			sink := New(slog.New(h))
			sink.Write(tt.level, "done", map[string]any{"k": "v"})

			if len(h.records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(h.records))
			}
			if h.records[0].Level != tt.want {
				t.Fatalf("level = %v, want %v", h.records[0].Level, tt.want)
			}
			if h.records[0].Message != "done" {
				t.Fatalf("message = %q, want %q", h.records[0].Message, "done")
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

type disabledSlogHandler struct {
	handled bool
}

func (*disabledSlogHandler) Enabled(context.Context, slog.Level) bool { return false }
func (h *disabledSlogHandler) Handle(context.Context, slog.Record) error {
	h.handled = true
	return nil
}
func (h *disabledSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *disabledSlogHandler) WithGroup(string) slog.Handler      { return h }

func TestSinkSkipsDisabledEvent(t *testing.T) {
	handler := &disabledSlogHandler{}
	sink := New(slog.New(handler))
	sink.Write(hc.LevelDebug, "debug", map[string]any{"k": "v"})
	if handler.handled {
		t.Fatal("disabled debug reached handler")
	}
}

type captureSlogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
	Order   []string
}

type captureSlogHandler struct {
	records []captureSlogRecord
}

func (h *captureSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	rec := captureSlogRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]any),
	}
	r.Attrs(func(attr slog.Attr) bool {
		rec.Attrs[attr.Key] = attr.Value.Any()
		rec.Order = append(rec.Order, attr.Key)
		return true
	})
	h.records = append(h.records, rec)
	return nil
}

func (h *captureSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(string) slog.Handler {
	return h
}
