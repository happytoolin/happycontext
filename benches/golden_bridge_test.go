package benches_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
	zerologadapter "github.com/happytoolin/happycontext/adapter/zerolog"
	"github.com/rs/zerolog"
)

// TestGoldenZerologBridgeParity is the bridge golden gate (V2_DESIGN
// §4): for a fixed corpus driven through real lifecycles, the
// first-party JSON sink and the zerolog bridge must emit the same
// PARSED field set. Equivalence is on parsed values, not bytes. The
// exception list: the time value (each sink stamps its own write).
func TestGoldenZerologBridgeParity(t *testing.T) {
	corpus := []struct {
		name   string
		mutate func(ctx context.Context)
		err    error
	}{
		{"plain", nil, nil},
		{"scalars", func(ctx context.Context) {
			hc.Add(ctx, "s", "v", "i", 7, "u8", uint8(9), "f", 2.5, "b", true)
		}, nil},
		{"temporal", func(ctx context.Context) {
			hc.Add(ctx, "t", time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), "d", 1500*time.Millisecond)
		}, nil},
		{"escapes", func(ctx context.Context) {
			hc.Add(ctx, "q", `she said "hi"`, "bs", `C:\path`, "ctl", "tab\ttab")
			hc.Add(ctx, "uni", "héllo ☃ 🍜", "nul", "x\x00y", "del", "d\x7f")
		}, nil},
		{"raw_json", func(ctx context.Context) {
			hc.AddRawJSON(ctx, "meta", []byte(`{"nested":true,"n":1}`))
		}, nil},
		{"any_fallback", func(ctx context.Context) {
			hc.Add(ctx, "obj", map[string]any{"a": 1}, "sl", []any{1, "x"}, "nil", nil)
		}, nil},
		{"duplicates", func(ctx context.Context) {
			hc.Add(ctx, "k", "first", "k", "second", "other", 1)
		}, nil},
		{"wide", func(ctx context.Context) {
			for i := 0; i < 32; i++ {
				hc.Add(ctx, "k"+strings.Repeat("x", i+1), i)
			}
		}, nil},
		{"error", nil, errors.New("boom")},
	}

	for _, c := range corpus {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// first-party JSON sink
			var hbuf bytes.Buffer
			hrt := hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(&hbuf), SamplingRate: 1})
			hop := hc.Start(context.Background(), hrt, hc.OperationStart{Domain: hc.DomainJob, Name: "golden"})
			if c.mutate != nil {
				c.mutate(hop.Context())
			}
			hop.End(&c.err)

			// zerolog bridge
			var zbuf bytes.Buffer
			zlogger := zerolog.New(&zbuf)
			zrt := hc.MustCompile(hc.Config{Sink: zerologadapter.New(&zlogger), SamplingRate: 1})
			zop := hc.Start(context.Background(), zrt, hc.OperationStart{Domain: hc.DomainJob, Name: "golden"})
			if c.mutate != nil {
				c.mutate(zop.Context())
			}
			zop.End(&c.err)

			assertGoldenParity(t, zbuf.Bytes(), hbuf.Bytes())
		})
	}
}

func assertGoldenParity(t *testing.T, zerologLine, hcLine []byte) {
	t.Helper()

	var z, h map[string]any
	if err := json.Unmarshal(zerologLine, &z); err != nil {
		t.Fatalf("zerolog bridge output not valid JSON: %v (%q)", err, zerologLine)
	}
	if err := json.Unmarshal(hcLine, &h); err != nil {
		t.Fatalf("JSON sink output not valid JSON: %v (%q)", err, hcLine)
	}

	if len(z) != len(h) {
		t.Fatalf("field count differs: zerolog %v vs hc %v", z, h)
	}
	for k, zv := range z {
		hv, ok := h[k]
		if !ok {
			t.Fatalf("field %q missing from JSON sink output", k)
		}
		if k == "time" {
			zs, ok1 := zv.(string)
			hs, ok2 := hv.(string)
			if !ok1 || !ok2 {
				t.Fatalf("time fields not strings: %v %v", zv, hv)
			}
			if _, err := time.Parse(time.RFC3339, zs); err != nil {
				t.Fatalf("bridge time %q not RFC3339", zs)
			}
			if _, err := time.Parse(time.RFC3339, hs); err != nil {
				t.Fatalf("sink time %q not RFC3339", hs)
			}
			continue
		}
		zj, _ := json.Marshal(zv)
		hj, _ := json.Marshal(hv)
		if string(zj) != string(hj) {
			t.Fatalf("field %q differs: zerolog %s vs hc %s", k, zj, hj)
		}
	}
}
