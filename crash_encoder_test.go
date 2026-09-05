package hc

// Agent C — encoder abuse. Everything hostile a user can push through
// Add/AddRawJSON and the field constructors: invalid UTF-8, control
// characters, cyclic values, huge and boundary-crossing field counts,
// envelope-key collisions, empty and unicode keys. Every case must
// yield a parseable canonical line or a pinned, documented failure —
// never a panic or a hang.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustParseLine(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("line not parseable: %v: %s", err, line)
	}
	return m
}

func TestCrashHostileStrings(t *testing.T) {
	cases := []string{
		"\xff\xfe raw high bytes",               // invalid UTF-8
		"\x00\x01\x1f control",                  // control characters
		"quote\"backslash\\newline\n\t\r",       // JSON metacharacters
		"\x7f\x80\xef\xbb\xbf bom",              // DEL, lone continuation, real BOM bytes
		"emoji \U0001F6A8 unicode \u2028\u2029", // line/para separators
		strings.Repeat("x", 1<<16),              // 64KB value
	}
	for _, tc := range cases {
		rec := recOf(LevelInfo, tc, fieldOf("v", tc), fieldOf("k"+tc, "key with hostile bytes"))
		line := rec.Encoded()
		m := mustParseLine(t, line)
		if m["message"] == nil {
			t.Fatalf("message missing for %q", tc[:min(len(tc), 20)])
		}
		// Round-trip: Go's decoder replaces invalid UTF-8 with U+FFFD;
		// anything valid must come back byte-identical.
		got, _ := m["v"].(string)
		if !strings.ContainsRune(tc, 0xFFFD) && got != tc && utf8.ValidString(tc) {
			t.Fatalf("round-trip mismatch: got %q want %q", got, tc)
		}
	}
}

func TestCrashCyclicAnyValue(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	rec := recOf(LevelWarn, "cycle", fieldOf("cyc", m))
	line := rec.Encoded() // must not hang
	parsed := mustParseLine(t, line)
	s, _ := parsed["cyc"].(string)
	if !strings.Contains(s, "cycle") && !strings.Contains(s, "error") {
		t.Fatalf("cyclic value = %#v, want a marshaling-error string", parsed["cyc"])
	}

	// Capture path (TestSink deep-copy) must also survive the cycle.
	rt, ts := testRT(t, nil)
	op := Start(nil2Ctx(), rt, OperationStart{Domain: DomainJob, Name: "j"})
	Add(op.Context(), "cyc", m)
	_ = op.End(nil)
	if len(ts.Events()) != 1 {
		t.Fatal("cyclic value dropped the event")
	}
}

func TestCrashFloatEdgeValues(t *testing.T) {
	vals := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64, math.MaxFloat64, -0.0}
	for _, v := range vals {
		rec := recOf(LevelInfo, "f", fieldOf("f", v))
		m := mustParseLine(t, rec.Encoded())
		if m["f"] == nil {
			t.Fatalf("float %v dropped from line", v)
		}
	}
	rec32 := recOf(LevelInfo, "f32", fieldOf("f32", float32(math.NaN())))
	m := mustParseLine(t, rec32.Encoded())
	if s, _ := m["f32"].(string); s != "NaN" {
		t.Fatalf("float32 NaN = %#v, want \"NaN\"", m["f32"])
	}
}

func TestCrashRawJSONInjection(t *testing.T) {
	// Contract: AddRawJSON appends verbatim, no sanitization. Pin that
	// invalid blobs produce an unparseable line WITHOUT panicking —
	// the caller owned those bytes. (KindRaw fields constructed
	// directly: recOf's fieldOf would type a []byte as KindAny, which
	// the encoder renders base64 — itself worth pinning.)
	rec := recOf(LevelInfo, "m", Field{key: "raw", kind: KindRaw, val: []byte(`{"broken":`)})
	if rec.Encoded() == nil {
		t.Fatal("encode returned nil")
	}
	if json.Valid(rec.Encoded()) {
		t.Fatalf("invalid raw unexpectedly produced valid JSON: %s", rec.Encoded())
	}
	rec2 := recOf(LevelInfo, "m", Field{key: "raw", kind: KindRaw, val: []byte(`}}}}} early-close`)})
	if rec2.Encoded() == nil {
		t.Fatal("encode returned nil")
	}
	// A plain []byte through Add renders base64 (json.Marshal shape).
	recB64 := recOf(LevelInfo, "m", fieldOf("bytes", []byte(`{"ok":[1,2]}`)))
	mb := mustParseLine(t, recB64.Encoded())
	if s, _ := mb["bytes"].(string); s != "eyJvayI6WzEsMl19" {
		t.Fatalf("[]byte not base64: %#v", mb["bytes"])
	}
	// Valid raw still embeds verbatim.
	rec3 := recOf(LevelInfo, "m", Field{key: "raw", kind: KindRaw, val: []byte(`{"ok":[1,2]}`)})
	m := mustParseLine(t, rec3.Encoded())
	raw, _ := m["raw"].(map[string]any)
	if raw == nil || raw["ok"] == nil {
		t.Fatalf("valid raw JSON not embedded: %s", rec3.Encoded())
	}
}

func TestCrashExtremeFieldCounts(t *testing.T) {
	for _, n := range []int{0, 1, 23, 24, 25, 1023, 1024, 1025, 5000} {
		fields := make([]Field, n)
		for i := range n {
			fields[i] = fieldOf(fmt.Sprintf("k%d", i), i)
		}
		rec := recOf(LevelInfo, "wide", fields...)
		m := mustParseLine(t, rec.Encoded())
		// Every key present exactly once.
		for i := range min(n, 30) {
			key := fmt.Sprintf("k%d", i)
			if m[key] == nil {
				t.Fatalf("n=%d: key %s missing", n, key)
			}
		}
		if got := len(m) - 3; n <= 1024 && got != n { // minus level/time/message
			t.Fatalf("n=%d: members = %d", n, got)
		}
	}
}

func TestCrashEnvelopeCollisions(t *testing.T) {
	cases := [][]string{
		{"time"},
		{"message"},
		{"level"},
		{"time", "message", "level"},
		{"fields.time"},
		{"fields.message", "message"},
		{"time", "time", "fields.time"},
		{"message", "fields.message", "fields.fields.message"},
	}
	for _, keys := range cases {
		var fields []Field
		for i, k := range keys {
			fields = append(fields, fieldOf(k, fmt.Sprintf("v%d", i)))
		}
		rec := recOf(LevelInfo, "real message", fields...)
		m := mustParseLine(t, rec.Encoded())
		// Envelope survives intact.
		if m["message"] != "real message" {
			t.Fatalf("keys %v: envelope message = %#v", keys, m["message"])
		}
		if m["level"] != "info" {
			t.Fatalf("keys %v: envelope level = %#v", keys, m["level"])
		}
		if _, ok := m["time"].(string); !ok {
			t.Fatalf("keys %v: envelope time missing", keys)
		}
		// Last aliased write wins.
		last := fmt.Sprintf("v%d", len(keys)-1)
		if keys[len(keys)-1] == "time" || keys[len(keys)-1] == "message" || keys[len(keys)-1] == "level" {
			aliased := "fields." + keys[len(keys)-1]
			if m[aliased] != last {
				t.Fatalf("keys %v: %s = %#v, want %s", keys, aliased, m[aliased], last)
			}
		}
	}
}

func TestCrashEmptyAndUnicodeKeys(t *testing.T) {
	rec := recOf(LevelInfo, "m",
		fieldOf("", "empty key"),
		fieldOf("日本語キー", "unicode"),
		fieldOf("k\x00with\xffjunk", "hostile key"),
	)
	m := mustParseLine(t, rec.Encoded())
	if v, _ := m["日本語キー"].(string); v != "unicode" {
		t.Fatalf("unicode key lost: %#v", m)
	}
}

func TestCrashTypedNilAndWeirdAny(t *testing.T) {
	type customErr struct{}
	var nilTypedErr *customErr
	rec := recOf(LevelInfo, "m",
		fieldOf("nilAny", nil),
		fieldOf("typedNil", nilTypedErr), // non-nil interface, nil pointer
		fieldOf("chan", make(chan int)),  // json.Marshal errors
		fieldOf("func", func() {}),
	)
	line := rec.Encoded()
	m := mustParseLine(t, line)
	if m["nilAny"] != nil {
		t.Fatalf("nilAny = %#v", m["nilAny"])
	}
	if m["chan"] == nil && m["func"] == nil {
		t.Fatal("both unmarshalable values vanished without a trace")
	}
}

func nil2Ctx() context.Context { return context.Background() }
