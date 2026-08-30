package slogadapter

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/happytoolin/happycontext"
)

type retainingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *retainingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *retainingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}

func (h *retainingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *retainingHandler) WithGroup(string) slog.Handler      { return h }

func TestSinkPooledAttrsSurviveRetainingHandler(t *testing.T) {
	h := &retainingHandler{}
	sink := New(slog.New(h))

	fields := map[string]any{"a": 1, "b": "two", "c": true}
	for i := 0; i < 100; i++ {
		sink.Write(hc.LevelInfo, "m", fields)
	}

	if len(h.records) != 100 {
		t.Fatalf("got %d records, want 100", len(h.records))
	}
	for i, r := range h.records {
		if r.Message != "m" {
			t.Fatalf("record %d: message = %q", i, r.Message)
		}
		count := 0
		r.Attrs(func(a slog.Attr) bool {
			count++
			switch a.Key {
			case "a":
				if a.Value.Int64() != 1 {
					t.Fatalf("record %d: a = %v", i, a.Value)
				}
			case "b":
				if a.Value.String() != "two" {
					t.Fatalf("record %d: b = %v", i, a.Value)
				}
			case "c":
				if a.Value.Bool() != true {
					t.Fatalf("record %d: c = %v", i, a.Value)
				}
			default:
				t.Fatalf("record %d: unexpected attr %q", i, a.Key)
			}
			return true
		})
		if count != 3 {
			t.Fatalf("record %d: %d attrs, want 3", i, count)
		}
	}
}

func TestSinkConcurrentWritesLargeMaps(t *testing.T) {
	h := &retainingHandler{}
	sink := New(slog.New(h))

	bigFields := func(tag string) map[string]any {
		m := make(map[string]any, 100)
		for i := 0; i < 100; i++ {
			m["k"+strconv.Itoa(i)] = tag + ":" + strconv.Itoa(i)
		}
		return m
	}

	const writers = 8
	const writes = 50

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			tag := "w" + strconv.Itoa(w)
			for i := 0; i < writes; i++ {
				sink.Write(hc.LevelInfo, tag, bigFields(tag))
			}
		}(w)
	}
	wg.Wait()

	total := writers * writes
	if len(h.records) != total {
		t.Fatalf("got %d records, want %d", len(h.records), total)
	}
	for i, r := range h.records {
		if len(r.Message) == 0 || !strings.HasPrefix(r.Message, "w") {
			t.Fatalf("record %d: corrupted message %q", i, r.Message)
		}
		tag := r.Message
		count := 0
		r.Attrs(func(a slog.Attr) bool {
			want := tag + ":" + strings.TrimPrefix(a.Key, "k")
			if a.Value.String() != want {
				t.Fatalf("record %d (%s): attr %s = %q, want %q (cross-writer contamination)",
					i, tag, a.Key, a.Value.String(), want)
			}
			count++
			return true
		})
		if count != 100 {
			t.Fatalf("record %d: %d attrs, want 100", i, count)
		}
	}
}

func TestSinkNilFieldsAndOddValues(t *testing.T) {
	h := &retainingHandler{}
	sink := New(slog.New(h))

	sink.Write(hc.LevelInfo, "nil_fields", nil)
	sink.Write(hc.LevelInfo, "nil_value", map[string]any{"x": nil})
	sink.Write(hc.LevelInfo, "", map[string]any{"empty_msg": 1})

	if len(h.records) != 3 {
		t.Fatalf("got %d records, want 3", len(h.records))
	}
	if r := h.records[0]; r.Message != "nil_fields" {
		t.Fatalf("record 0 message = %q", r.Message)
	}
	if r := h.records[1]; r.NumAttrs() != 1 {
		t.Fatalf("record 1: %d attrs, want 1", r.NumAttrs())
	}
	if r := h.records[2]; r.Message != hc.DefaultMessage {
		t.Fatalf("empty message should fall back to %q, got %q", hc.DefaultMessage, r.Message)
	}
}

func TestSinkRecoverableAfterHandlerPanic(t *testing.T) {
	var calls int
	boom := slog.New(panicHandler{calls: &calls})
	sink := New(boom)

	for i := 0; i < 3; i++ {
		func() {
			defer func() { _ = recover() }()
			sink.Write(hc.LevelInfo, "boom", map[string]any{"i": i})
		}()
	}
	if calls != 3 {
		t.Fatalf("handler called %d times, want 3", calls)
	}

	h := &retainingHandler{}
	ok := New(slog.New(h))
	ok.Write(hc.LevelInfo, "after", map[string]any{"k": "v"})
	if len(h.records) != 1 || h.records[0].Message != "after" {
		t.Fatalf("sink unusable after handler panic: %v", h.records)
	}
}

func TestRecycleSliceClearsAndCaps(t *testing.T) {
	attrs := make([]slog.Attr, 0, slogPoolCapacity)
	for range 100 {
		attrs = append(attrs, slog.Attr{})
	}
	if cap(attrs) > slogPoolMaxCapacity {
		t.Fatalf("100-field buffer exceeds pool limit: cap=%d limit=%d", cap(attrs), slogPoolMaxCapacity)
	}
	attrs[0] = slog.Any("retained", new(int))
	var attrPool sync.Pool
	recycleSlice(&attrPool, &attrs, attrs)
	first := attrs[:cap(attrs)][0]
	if len(attrs) != 0 || first.Key != "" || first.Value.Any() != nil {
		t.Fatalf("grown attr buffer was not cleared and recycled: len=%d first=%v", len(attrs), first)
	}

	var pool sync.Pool
	oversized := make([]any, slogPoolMaxCapacity+1)
	oversized[0] = new(int)
	recycleSlice(&pool, &oversized, oversized)
	if len(oversized) != slogPoolMaxCapacity+1 {
		t.Fatalf("oversized buffer was retained: len=%d", len(oversized))
	}
}

type panicHandler struct{ calls *int }

func (p panicHandler) Enabled(context.Context, slog.Level) bool { return true }
func (p panicHandler) Handle(_ context.Context, _ slog.Record) error {
	*p.calls++
	panic("boom")
}
func (p panicHandler) WithAttrs([]slog.Attr) slog.Handler { return p }
func (p panicHandler) WithGroup(string) slog.Handler      { return p }
