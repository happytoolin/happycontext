package hc

// Agent H — deep and wide payloads. Non-cyclic deep nesting (the
// cyclic case is pinned in round 1), mega field counts, and huge
// strings: encode, capture, and pool must all survive.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func nest(depth int, leaf any) map[string]any {
	m := map[string]any{"leaf": leaf}
	for range depth {
		m = map[string]any{"n": m}
	}
	return m
}

func TestCrashDeepNesting(t *testing.T) {
	for _, depth := range []int{100, 1000, 10000} {
		v := nest(depth, "bottom")
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("depth %d panicked: %v", depth, r)
				}
			}()
			rec := recOf(LevelInfo, "deep", fieldOf("nest", v))
			_ = mustParseLine(t, rec.Encoded())

			// The TestSink deep-copy path must also survive.
			rt, ts := testRT(t, nil)
			op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
			Add(op.Context(), "nest", v)
			_ = op.End(nil)
			if len(ts.Events()) != 1 {
				t.Fatalf("depth %d dropped", depth)
			}
		}()
	}
}

func TestCrashMegaFields(t *testing.T) {
	const n = 100000
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "mega"})
	for i := range n {
		Add(op.Context(), fmt.Sprintf("f%d", i), i)
	}
	if !op.End(nil) {
		t.Fatal("mega event dropped")
	}
	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	// Spot check first/last fields through the encoder.
	var fields []Field
	for _, f := range evs[0].Fields() {
		fields = append(fields, f)
	}
	rec := recOf(LevelInfo, "mega", fields...)
	line := rec.Encoded()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("mega line unparseable: %v", err)
	}
	if v, _ := m["f0"].(float64); v != 0 {
		t.Fatalf("f0 = %v", m["f0"])
	}
	if v, _ := m[fmt.Sprintf("f%d", n-1)].(float64); v != float64(n-1) {
		t.Fatalf("f%d = %v", n-1, m[fmt.Sprintf("f%d", n-1)])
	}
}

func TestCrashHugeString(t *testing.T) {
	big := strings.Repeat("payload\xef\xbb\xbf", 1<<20) // ~5MB
	rt, ts := testRT(t, nil)
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "big"})
	Add(op.Context(), "big", big, "sib", 1)
	_ = op.End(nil)
	rec := recOf(LevelInfo, "big", evs2Fields(t, ts)...)
	line := rec.Encoded()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("huge line unparseable: %v", err)
	}
	if got, _ := m["big"].(string); len(got) < (1<<20)*6-8 {
		t.Fatalf("huge string truncated: %d", len(got))
	}
}

func evs2Fields(t *testing.T, ts *TestSink) []Field {
	t.Helper()
	evs := ts.Events()
	if len(evs) != 1 {
		t.Fatalf("events = %d", len(evs))
	}
	return evs[0].Fields()
}

// A few concurrent mega events: memory-bounded stress through the
// pool, exact isolation.
func TestCrashConcurrentMegaEvents(t *testing.T) {
	rt, ts := testRT(t, nil)
	done := make(chan int, 4)
	for w := range 4 {
		go func(w int) {
			defer func() { done <- w }()
			for i := range 3 {
				op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "m"})
				for j := range 20000 {
					Add(op.Context(), fmt.Sprintf("w%d-i%d-f%d", w, i, j), j)
				}
				_ = op.End(nil)
			}
		}(w)
	}
	for range 4 {
		<-done
	}
	if got := len(ts.Events()); got != 12 {
		t.Fatalf("events = %d, want 12", got)
	}
	for _, ev := range ts.Events() {
		if len(ev.Fields()) < 20000 {
			t.Fatalf("event shrank: %d fields", len(ev.Fields()))
		}
	}
}
