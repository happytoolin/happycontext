package slogadapter

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestSinkWriteMapsLevelAndDefaultsMessage(t *testing.T) {
	h := &captureSlogHandler{}
	logger := slog.New(h)
	sink := New(logger)

	sink.Write(context.Background(), "WARN", "", map[string]any{
		"user_id": "u_1",
	})

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}
	if h.records[0].Message != "request_completed" {
		t.Fatalf("expected default message, got %q", h.records[0].Message)
	}
	if h.records[0].Level != slog.LevelWarn {
		t.Fatalf("expected warn level, got %v", h.records[0].Level)
	}
	if h.records[0].Attrs["user_id"] != "u_1" {
		t.Fatalf("missing user_id attr")
	}
}

func TestSinkDeterministicOrderSortsKeys(t *testing.T) {
	h := &captureSlogHandler{}
	logger := slog.New(h)
	sink := NewWithOptions(logger, SinkOptions{DeterministicOrder: true})

	sink.Write(context.Background(), "INFO", "done", map[string]any{
		"z": 1,
		"a": 2,
		"m": 3,
	})

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}
	expectedOrder := []string{"a", "m", "z"}
	if !slices.Equal(h.records[0].Order, expectedOrder) {
		t.Fatalf("expected sorted attrs order %v, got %v", expectedOrder, h.records[0].Order)
	}
}

func TestSinkWriteMapsAllKnownLevels(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{name: "debug", level: hc.LevelDebug, want: slog.LevelDebug},
		{name: "warn", level: hc.LevelWarn, want: slog.LevelWarn},
		{name: "error", level: hc.LevelError, want: slog.LevelError},
		{name: "default", level: "UNKNOWN", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &captureSlogHandler{}
			sink := New(slog.New(h))
			sink.Write(context.Background(), tt.level, "done", map[string]any{"k": "v"})

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
	nilSink.Write(context.Background(), hc.LevelInfo, "x", map[string]any{"k": 1})

	sink := New(nil)
	sink.Write(context.Background(), hc.LevelInfo, "x", map[string]any{"k": 1})
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
