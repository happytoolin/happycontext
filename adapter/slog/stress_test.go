package slogadapter

import (
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

// BenchmarkStressParallelWrite drives the pooled attrs path from all cores
// through a real JSON handler, the closest analogue to production load.
func BenchmarkStressParallelWrite(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sink := New(logger)
	fields := func() map[string]any {
		m := make(map[string]any, 15)
		for i := 0; i < 15; i++ {
			m["k"+strconv.Itoa(i)] = i
		}
		m["http.status"] = 200
		m["feature"] = "checkout"
		return m
	}()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sink.Write(hc.LevelInfo, "request_completed", fields)
		}
	})
}

func BenchmarkStressParallelDeterministic(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	sink := NewWithOptions(logger, SinkOptions{DeterministicOrder: true})
	fields := func() map[string]any {
		m := make(map[string]any, 15)
		for i := 0; i < 15; i++ {
			m["k"+strconv.Itoa(i)] = i
		}
		return m
	}()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sink.Write(hc.LevelInfo, "request_completed", fields)
		}
	})
}

// TestStressSlogSustainedCorrectness soaks the sink under sustained parallel
// load and verifies every record survived intact (retained, not cloned).
func TestStressSlogSustainedCorrectness(t *testing.T) {
	h := &retainingHandler{}
	sink := New(slog.New(h))
	fields := map[string]any{"a": 1, "b": "two", "c": true, "d": nil}

	const goroutines = 8
	const writes = 20_000

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writes; i++ {
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
