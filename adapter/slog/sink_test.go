package slogadapter

import (
	"context"
	"log/slog"
	"testing"

	"github.com/happytoolin/happycontext"
)

func emit(t *testing.T, sink hc.Sink, level hc.Level, kv ...any) {
	t.Helper()
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.SetLevel(op.Context(), level)
	if len(kv) > 0 {
		hc.Add(op.Context(), kv[0].(string), kv[1], kv[2:]...)
	}
	op.End(nil)
}

func TestSinkWriteMapsLevelAndMessage(t *testing.T) {
	h := &captureSlogHandler{}
	emit(t, New(slog.New(h)), hc.LevelWarn, "user_id", "u_1")

	if len(h.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(h.records))
	}
	if h.records[0].Message != hc.DefaultOperationMessage {
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
	// debug arrives via a domain policy, warn via the requested floor,
	// error via the error outcome — each must map to its slog level.
	t.Run("debug", func(t *testing.T) {
		h := &captureSlogHandler{}
		rt := hc.MustCompile(hc.Config{
			Sink:         New(slog.New(h)),
			SamplingRate: 1,
			OperationPolicies: map[hc.Domain]hc.OperationPolicy{
				hc.DomainJob: {SuccessLevel: hc.LevelDebug},
			},
		})
		hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"}).End(nil)
		if h.records[0].Level != slog.LevelDebug {
			t.Fatalf("level = %v, want DEBUG", h.records[0].Level)
		}
	})
	t.Run("warn", func(t *testing.T) {
		h := &captureSlogHandler{}
		op := hc.Start(context.Background(), mustRT(t, New(slog.New(h))), hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		hc.SetLevel(op.Context(), hc.LevelWarn)
		op.End(nil)
		if h.records[0].Level != slog.LevelWarn {
			t.Fatalf("level = %v, want WARN", h.records[0].Level)
		}
	})
	t.Run("error", func(t *testing.T) {
		h := &captureSlogHandler{}
		var err error = errBoom{}
		op := hc.Start(context.Background(), mustRT(t, New(slog.New(h))), hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		op.End(&err)
		if h.records[0].Level != slog.LevelError {
			t.Fatalf("level = %v, want ERROR", h.records[0].Level)
		}
	})
}

func mustRT(t *testing.T, sink hc.Sink) *hc.Runtime {
	t.Helper()
	return hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

// TestSinkTypedAttrs pins the typed-constructor mapping: insertion
// order is preserved and kinds survive (int64, bool, duration).
func TestSinkTypedAttrs(t *testing.T) {
	h := &captureSlogHandler{}
	emit(t, New(slog.New(h)), hc.LevelInfo,
		"s", "v", "i", 7, "b", true, "d", 1500*int64(1000000))

	rec := h.records[0]
	if got := rec.Attrs["i"]; got != int64(7) {
		t.Fatalf("i = %v (%T)", got, got)
	}
	if got := rec.Attrs["b"]; got != true {
		t.Fatalf("b = %v", got)
	}
	if _, ok := rec.Attrs["s"]; !ok {
		t.Fatal("missing s")
	}
	// insertion order preserved: op.domain, op.name, s, i, b, d, ...
	if rec.Order[2] != "s" || rec.Order[3] != "i" {
		t.Fatalf("order = %v", rec.Order)
	}
}

// TestSinkDedupesLastWriteWins mirrors the core encode-side resolution.
func TestSinkDedupesLastWriteWins(t *testing.T) {
	h := &captureSlogHandler{}
	rt := hc.MustCompile(hc.Config{Sink: New(slog.New(h)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "k", "first", "k", "second")
	op.End(nil)

	if got := h.records[0].Attrs["k"]; got != "second" {
		t.Fatalf("k = %v", got)
	}
	count := 0
	for _, k := range h.records[0].Order {
		if k == "k" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("k emitted %d times", count)
	}
}

// TestSinkErrorAndRawWireFidelity pins the v0 shapes: error fields
// render the message string (never null); raw bytes render via slog.Any.
func TestSinkErrorAndRawWireFidelity(t *testing.T) {
	h := &captureSlogHandler{}
	rt := hc.MustCompile(hc.Config{Sink: New(slog.New(h)), SamplingRate: 1})
	op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
	hc.Add(op.Context(), "e", errBoom{})
	hc.AddRawJSON(op.Context(), "meta", []byte(`{"raw":true}`))
	op.End(nil)

	if got := h.records[0].Attrs["e"]; got != "boom" {
		t.Fatalf("error field = %v (%T), want \"boom\"", got, got)
	}
	if got, ok := h.records[0].Attrs["meta"]; !ok || got == nil {
		t.Fatalf("raw field = %v, want non-nil (slog.Any bytes)", got)
	}
}

func TestSinkWriteNilSafety(t *testing.T) {
	var nilSink *Sink
	nilSink.Write(context.Background(), nil)

	sink := New(nil)
	sink.Write(context.Background(), nil)
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
	emit(t, New(slog.New(handler)), hc.LevelDebug)
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

func (h *captureSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureSlogHandler) WithGroup(string) slog.Handler      { return h }
