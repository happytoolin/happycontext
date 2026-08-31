package benches_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
	zerologadapter "github.com/happytoolin/happycontext/adapter/zerolog"
	"github.com/rs/zerolog"
)

// TestJSONSinkGoldenZerologParity is the v0.6 golden gate
// (openspec json-encoder delta, V2_DESIGN §4): for a fixed corpus, the
// first-party JSON sink and the current zerolog adapter (wired the way the
// README documents, zerolog.New(w).With().Timestamp().Logger()) must emit
// the same parsed field set. Equivalence is on parsed values, not bytes —
// v0 adapters iterate a map, so ordering is random by contract. The only
// expected exception is the `time` value itself (each sink stamps its own
// write time); it is compared for presence, RFC3339 shape, and proximity.
func TestJSONSinkGoldenZerologParity(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 34, 56, 0, time.UTC)
	offsetZone := time.FixedZone("JST", 9*3600)

	corpus := []struct {
		name    string
		level   hc.Level
		message string
		fields  map[string]any
	}{
		{
			name:    "empty_fields",
			level:   hc.LevelInfo,
			message: "request_completed",
		},
		{
			name:    "default_message",
			level:   hc.LevelInfo,
			message: "",
		},
		{
			name:    "all_levels",
			level:   hc.LevelWarn,
			message: "four events, one per level, are emitted by the loop below",
		},
		{
			name:    "typed_numerics",
			level:   hc.LevelInfo,
			message: "typed",
			fields: map[string]any{
				"f32_nan": float32(math.NaN()),
				"i":       int(-1),
				"i8":      int8(-128),
				"i16":     int16(-32768),
				"i32":     int32(-2147483648),
				"i64":     int64(-9223372036854775808),
				"u":       uint(42),
				"u8":      uint8(255),
				"u16":     uint16(65535),
				"u32":     uint32(4294967295),
				"u64":     uint64(18446744073709551615),
				"b":       true,
			},
		},
		{
			name:    "float_edges",
			level:   hc.LevelInfo,
			message: "floats",
			fields: map[string]any{
				"f_zero":    0.0,
				"f_frac":    0.000001,
				"f_small":   1e-7,
				"f_big":     1e21,
				"f_neg":     -2.5,
				"f32":       float32(1.5),
				"f32_tiny":  float32(1e-10), // 'e' notation on the 32-bit path
				"f32_huge":  float32(1e22),  // 'e' notation on the 32-bit path
				"f_nan":     math.NaN(),
				"f_pinf":    math.Inf(1),
				"f_ninf":    math.Inf(-1),
				"f_precise": 0.1,
			},
		},
		{
			name:    "time_and_duration",
			level:   hc.LevelInfo,
			message: "temporal",
			fields: map[string]any{
				"utc":         now,
				"offset_zone": now.In(offsetZone),
				"dur_ms":      1500 * time.Millisecond,
				"dur_frac":    123456789 * time.Nanosecond,
				"dur_zero":    time.Duration(0),
			},
		},
		{
			name:    "error_shapes",
			level:   hc.LevelError,
			message: "failed",
			fields: map[string]any{
				"error":       errors.New("plain error"),
				"error_value": structuredErrorForGolden(errors.New("boom")),
			},
		},
		{
			name:    "strings_and_escapes",
			level:   hc.LevelInfo,
			message: "strings",
			fields: map[string]any{
				"empty":          "",
				"quote":          `she said "hi"`,
				"backslash":      `C:\path\to\file`,
				"control":        "tab\tnewline\ncr\r",
				"nul":            "x\x00y",
				"del":            "del\x7f",
				"invalid_utf8":   "broken\xc3(broken",
				"unicode":        "héllo ☃ 日本語 🍜 😀",
				"long_clean":     "/api/v1/users/12345/orders?include=items&fields=all&cursor=abcdefghijklmnopqrstuvwxyz0123456789A",
				"long_escaped":   strings.Repeat(`quo"te\`, 8),
				"carry_chain_ff": string([]byte{0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe, 0x41}),
			},
		},
		{
			name:    "user_time_field",
			level:   hc.LevelInfo,
			message: "shadowed time",
			fields: map[string]any{
				// a user field named "time" must be shadowed the same way
				// on both sinks (zerolog's Timestamp hook and our stamp
				// both emit after the fields, last write wins)
				"time":        "2020-01-01T00:00:00Z",
				"observed_at": "start",
			},
		},
		{
			name:    "long_escaped_key_and_message",
			level:   hc.LevelWarn,
			message: `message with "quotes" and \backslashes\ long enough for the SWAR path`,
			fields: map[string]any{
				`key with "quote" and spaces long enough for SWAR`: "v",
				"k": "short",
			},
		},
		{
			name:    "any_fallback",
			level:   hc.LevelInfo,
			message: "any",
			fields: map[string]any{
				"nilv":       nil,
				"slice":      []any{1, "two", true},
				"object":     map[string]any{"b": 2, "a": 1},
				"empty_map":  map[string]any{},
				"byte_slice": []byte("binary\x00bytes"),
				"html":       map[string]any{"note": "<b>a&b</b>"},
			},
		},
		{
			name:    "wide_event_32",
			level:   hc.LevelInfo,
			message: "wide",
			fields:  jsonSinkFields32(),
		},
	}

	levels := []hc.Level{hc.LevelDebug, hc.LevelInfo, hc.LevelWarn, hc.LevelError, hc.Level("TRACE")}

	for _, c := range corpus {
		c := c
		runLevels := []hc.Level{c.level}
		if c.name == "all_levels" {
			runLevels = levels
		}
		for _, level := range runLevels {
			t.Run(c.name+"/"+string(level), func(t *testing.T) {
				// zerolog side, wired as the README documents
				var zbuf bytes.Buffer
				zlogger := zerolog.New(&zbuf).With().Timestamp().Logger()
				zsink := zerologadapter.New(&zlogger)
				zsink.Write(level, c.message, cloneMap(c.fields))

				// first-party side
				var hbuf bytes.Buffer
				hsink := hc.NewJSONSink(&hbuf)
				hsink.Write(level, c.message, cloneMap(c.fields))

				assertGoldenParity(t, zbuf.Bytes(), hbuf.Bytes())
			})
		}
	}
}

func assertGoldenParity(t *testing.T, zerologLine, hcLine []byte) {
	t.Helper()

	var z, h map[string]any
	if err := json.Unmarshal(zerologLine, &z); err != nil {
		t.Fatalf("zerolog adapter output not valid JSON: %v (%q)", err, zerologLine)
	}
	if err := json.Unmarshal(hcLine, &h); err != nil {
		t.Fatalf("JSON sink output not valid JSON: %v (%q)", err, hcLine)
	}

	// field sets must match exactly
	if len(z) != len(h) {
		t.Fatalf("field count differs: zerolog %v vs hc %v", z, h)
	}
	for k, zv := range z {
		hv, ok := h[k]
		if !ok {
			t.Fatalf("field %q missing from JSON sink output (zerolog: %v)", k, zv)
		}
		if k == "time" {
			// exception list: values are each sink's own write time
			zs, ok1 := zv.(string)
			hs, ok2 := hv.(string)
			if !ok1 || !ok2 {
				t.Fatalf("time fields not strings: zerolog %v hc %v", zv, hv)
			}
			zt, err1 := time.Parse(time.RFC3339, zs)
			ht, err2 := time.Parse(time.RFC3339, hs)
			if err1 != nil || err2 != nil {
				t.Fatalf("time fields not RFC3339: zerolog %q hc %q", zs, hs)
			}
			if diff := zt.Sub(ht); diff < -time.Minute || diff > time.Minute {
				t.Fatalf("time values not close: zerolog %v hc %v", zt, ht)
			}
			continue
		}
		// compare through JSON so float/number representations are
		// normalized identically for both sides
		zj, _ := json.Marshal(zv)
		hj, _ := json.Marshal(hv)
		if string(zj) != string(hj) {
			t.Fatalf("field %q differs: zerolog %s vs hc %s", k, zj, hj)
		}
	}
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func structuredErrorForGolden(err error) map[string]any {
	return map[string]any{
		"message": err.Error(),
		"type":    "*errors.errorString",
	}
}
