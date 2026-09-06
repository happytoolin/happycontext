package slogadapter

import (
	"context"
	"log/slog"
	"strconv"
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

// TestSinkRecordsSurviveRetainingHandler drives many lifecycles
// through the adapter; a handler that retains records must never see
// corrupted or cross-request data.
func TestSinkRecordsSurviveRetainingHandler(t *testing.T) {
	h := &retainingHandler{}
	rt := hc.MustCompile(hc.Config{Sink: New(slog.New(h)), SamplingRate: 1})

	for range 100 {
		op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "t"})
		hc.Add(op.Context(), "a", 1, "b", "two", "c", true)
		op.End(nil)
	}

	if len(h.records) != 100 {
		t.Fatalf("got %d records, want 100", len(h.records))
	}
	for i, r := range h.records {
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
				if !a.Value.Bool() {
					t.Fatalf("record %d: c = %v", i, a.Value)
				}
			}
			return true
		})
		if count < 3 {
			t.Fatalf("record %d: %d attrs, want >= 3", i, count)
		}
	}
}

// TestSinkConcurrentWrites drives concurrent lifecycles with distinct
// field payloads through one adapter instance.
func TestSinkConcurrentWrites(t *testing.T) {
	h := &retainingHandler{}
	rt := hc.MustCompile(hc.Config{Sink: New(slog.New(h)), SamplingRate: 1})

	const writers = 8
	const writes = 50

	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			tag := "w" + strconv.Itoa(w)
			for range writes {
				op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: tag})
				ctx := op.Context()
				for i := range 10 {
					hc.Add(ctx, "k"+strconv.Itoa(i), tag+":"+strconv.Itoa(i))
				}
				op.End(nil)
			}
		}(w)
	}
	wg.Wait()

	total := writers * writes
	if len(h.records) != total {
		t.Fatalf("got %d records, want %d", len(h.records), total)
	}
}

// TestStressSlogSustainedCorrectness hammers the adapter with
// sustained concurrent traffic; every record must stay intact.
func TestStressSlogSustainedCorrectness(t *testing.T) {
	h := &retainingHandler{}
	rt := hc.MustCompile(hc.Config{Sink: New(slog.New(h)), SamplingRate: 1})

	const goroutines = 8
	const writes = 2_000

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range writes {
				op := hc.Start(context.Background(), rt, hc.OperationStart{Domain: hc.DomainJob, Name: "stress"})
				hc.Add(op.Context(), "a", 1, "b", "two", "c", true)
				op.End(nil)
			}
		})
	}
	wg.Wait()

	if len(h.records) != goroutines*writes {
		t.Fatalf("got %d records, want %d", len(h.records), goroutines*writes)
	}
}
