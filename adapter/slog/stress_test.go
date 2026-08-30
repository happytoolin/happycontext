package slogadapter

import (
	"log/slog"
	"sync"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

func TestStressSlogSustainedCorrectness(t *testing.T) {
	h := &retainingHandler{}
	sink := New(slog.New(h))
	fields := map[string]any{"a": 1, "b": "two", "c": true, "d": nil}

	const goroutines = 8
	const writes = 20_000

	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range writes {
				sink.Write(hc.LevelInfo, "stress", fields)
			}
		}()
	}
	wg.Wait()

	total := goroutines * writes
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.records) != total {
		t.Fatalf("retained %d records, want %d", len(h.records), total)
	}
	for i, r := range h.records {
		if r.NumAttrs() != 4 {
			t.Fatalf("record %d: %d attrs, want 4", i, r.NumAttrs())
		}
	}
}
