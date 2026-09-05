package hc

// Agent K — adversarial fuzz targets. Seeds carry every hostile shape
// the crash suites found: cycles, deep nesting, NaN/Inf, funcs and
// chans, typed nils, invalid UTF-8, huge strings, envelope collisions.
// Oracles: no panic, and the emitted line is valid JSON (raw bytes
// never enter through these paths).

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func FuzzCrashAdversarialValues(f *testing.F) {
	f.Add("plain", 0)
	f.Add("\xff\xfe\x00\x1f\"\n\\", 1)
	f.Add(strings.Repeat("x", 100000), 2)
	f.Add("time", 3)
	f.Add("message", 4)
	f.Add("fields.time", 5)
	f.Add("", 6)
	f.Add("日本語\xef\xbb\xbf", 7)

	f.Fuzz(func(t *testing.T, key string, variant int) {
		var val any
		switch variant % 12 {
		case 0:
			val = t.Name()
		case 1:
			val = math.NaN()
		case 2:
			val = math.Inf(-1)
		case 3:
			m := map[string]any{"self": nil}
			m["self"] = m // cycle
			val = m
		case 4:
			val = nest(200, "leaf")
		case 5:
			val = func() {} // unmarshalable
		case 6:
			val = make(chan int)
		case 7:
			var nilPtr *strings.Builder
			val = nilPtr
		case 8:
			val = strings.Repeat("\xff\x00", 5000)
		case 9:
			val = []any{math.MaxFloat64, math.SmallestNonzeroFloat64, -0.0, nil, true}
		case 10:
			val = time.Date(-5, 13, 40, 25, 61, 61, -1, time.UTC)
		case 11:
			val = map[string]any{"": map[string]any{"nested": []any{map[string]any{"deep": 1}}}}
		}

		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "j"})
		Add(op.Context(), key, val, "sib", 1)
		emitted := op.End(nil)
		if !emitted || len(ts.Events()) != 1 {
			t.Fatalf("event dropped: variant %d key %q", variant, key)
		}
		rec := recOf(LevelInfo, "m", evs2Fields(t, ts)...)
		line := rec.Encoded()
		if !json.Valid(line) {
			t.Fatalf("invalid line (variant %d): %s", variant, truncate(line, 200))
		}
	})
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// FuzzCrashArmedInterleavings: random interleavings of guarded writes,
// setters, and a single End on an armed event. Oracle: exactly one
// event, valid line, no lost completion fields.
func FuzzCrashArmedInterleavings(f *testing.F) {
	f.Add(0, 0, 0, 0)
	f.Add(1, 10, 3, 7)
	f.Add(2, 0, 0, 1)
	f.Fuzz(func(t *testing.T, variant, writes, setters, errorsN int) {
		if writes > 64 {
			writes = 64
		}
		if setters > 16 {
			setters = 16
		}
		if errorsN > 16 {
			errorsN = 16
		}
		rt, ts := testRT(t, nil)
		op := Start(context.Background(), rt, OperationStart{Domain: DomainHTTP, Name: "request"})
		if variant%2 == 0 {
			op.ev.arm()
		}
		for i := range writes {
			Add(op.Context(), key2(i), i)
		}
		for i := range setters {
			if i%2 == 0 {
				SetLevel(op.Context(), LevelWarn)
			} else {
				SetMessage(op.Context(), "override")
			}
		}
		for range errorsN {
			Error(op.Context(), errBoom2{})
		}
		_ = op.End(nil)
		if variant%3 == 0 {
			Add(op.Context(), "straggler", 1) // post-seal no-op
		}
		evs := ts.Events()
		if len(evs) != 1 {
			t.Fatalf("events = %d, want 1", len(evs))
		}
		if _, ok := evs[0].Lookup("straggler"); ok {
			t.Fatal("post-seal write landed")
		}
		rec := recOf(evs[0].Level(), evs[0].Message(), evs[0].Fields()...)
		line := string(rec.Encoded())
		for _, want := range []string{`"op.outcome"`, `"duration_ms"`} {
			if !strings.Contains(line, want) {
				t.Fatalf("missing %s: %s", want, truncate([]byte(line), 200))
			}
		}
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid line: %s", truncate([]byte(line), 200))
		}
	})
}

func key2(i int) string { return "k" + strings.Repeat("p", i%8) + string(rune('a'+i%26)) }

type errBoom2 struct{}

func (errBoom2) Error() string { return "boom2" }
