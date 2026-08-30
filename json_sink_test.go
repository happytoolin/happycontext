package hc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONSinkOutputShape(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONSink(&buf)

	sink.Write(LevelError, "job failed", map[string]any{
		"op.domain":   "job",
		"op.code":     500,
		"duration_ms": 1234,
	})

	line := buf.Bytes()
	if !bytes.HasSuffix(line, []byte("}\n")) {
		t.Fatalf("line must end with }\\n, got %q", line)
	}
	if bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("expected exactly one line, got %q", line)
	}

	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		t.Fatalf("invalid JSON: %v (%q)", err, line)
	}
	if payload["level"] != "error" {
		t.Fatalf("level = %v, want lowercase error", payload["level"])
	}
	if payload["message"] != "job failed" {
		t.Fatalf("message = %v", payload["message"])
	}
	if payload["op.code"] != float64(500) {
		t.Fatalf("op.code = %v", payload["op.code"])
	}
	ts, ok := payload["time"].(string)
	if !ok {
		t.Fatalf("time missing or not a string: %v", payload["time"])
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Fatalf("time %q is not RFC3339: %v", ts, err)
	}
	if len(payload) != 6 {
		t.Fatalf("unexpected field count: %v", payload)
	}
}

func TestJSONSinkLevelCasingAndDefault(t *testing.T) {
	cases := map[Level]string{
		LevelDebug:     "debug",
		LevelInfo:      "info",
		LevelWarn:      "warn",
		LevelError:     "error",
		Level("BOGUS"): "info", // unknown levels fall back to info, like the adapter
	}
	for level, want := range cases {
		var buf bytes.Buffer
		NewJSONSink(&buf).Write(level, "m", nil)
		var payload map[string]any
		if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
			t.Fatalf("level %v: invalid JSON %q", level, buf.Bytes())
		}
		if payload["level"] != want {
			t.Fatalf("level %v rendered %v, want %q", level, payload["level"], want)
		}
		if payload["message"] != "m" {
			t.Fatalf("message = %v", payload["message"])
		}
	}
}

// TestJSONSinkTimeOrdering pins the completion-time stamp after the
// fields (matching zerolog's Timestamp hook) so a user field named "time"
// is shadowed identically by both sinks.
func TestJSONSinkTimeOrdering(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONSink(&buf)
	sink.Write(LevelInfo, "m", map[string]any{"user_time": "x", "zz_last": 1})
	line := buf.String()
	timeIdx := strings.Index(line, `"time":`)
	userIdx := strings.Index(line, `"user_time":`)
	lastIdx := strings.Index(line, `"zz_last":`)
	msgIdx := strings.Index(line, `"message":`)
	if timeIdx < 0 || userIdx < 0 || lastIdx < 0 || msgIdx < 0 {
		t.Fatalf("missing keys in %q", line)
	}
	if !(userIdx < timeIdx && lastIdx < timeIdx && timeIdx < msgIdx) {
		t.Fatalf("time must come after fields and before message: %q", line)
	}

	// a user field literally named "time" is shadowed by the sink stamp
	buf.Reset()
	sink.Write(LevelInfo, "m", map[string]any{"time": "2020-01-01T00:00:00Z"})
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	ts, ok := payload["time"].(string)
	if !ok || ts == "2020-01-01T00:00:00Z" {
		t.Fatalf("user time not shadowed by sink stamp: %v", payload["time"])
	}
	stamp, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("shadowing time %q not RFC3339: %v", ts, err)
	}
	if stamp.Year() < 2025 {
		t.Fatalf("shadowing time %q suspiciously old (clock manipulated?)", ts)
	}
}

func TestJSONSinkDefaultMessage(t *testing.T) {
	var buf bytes.Buffer
	NewJSONSink(&buf).Write(LevelInfo, "", nil)
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["message"] != DefaultMessage {
		t.Fatalf("message = %v, want %q", payload["message"], DefaultMessage)
	}
}

func TestJSONSinkFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	sink := NewJSONSink(&buf)
	sink.Write(LevelInfo, "typed", map[string]any{
		"s":    "x",
		"i":    int(1),
		"i8":   int8(2),
		"i16":  int16(3),
		"i32":  int32(4),
		"i64":  int64(5),
		"u":    uint(6),
		"u8":   uint8(7),
		"u16":  uint16(8),
		"u32":  uint32(9),
		"u64":  uint64(10),
		"f32":  float32(1.5),
		"f64":  2.25,
		"b":    true,
		"t":    now,
		"d":    2500 * time.Microsecond,
		"e":    errors.New("boom"),
		"nilv": nil,
		"m":    map[string]any{"nested": true},
		"esc":  strings.Repeat("a", 20) + "\"quoted\"\x7f",
	})

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	checks := map[string]any{
		"s": "x", "i": 1.0, "i8": 2.0, "i16": 3.0, "i32": 4.0, "i64": 5.0,
		"u": 6.0, "u8": 7.0, "u16": 8.0, "u32": 9.0, "u64": 10.0,
		"f32": 1.5, "f64": 2.25, "b": true,
		"t":    now.Format(time.RFC3339),
		"d":    2.5, // float milliseconds, zerolog default
		"e":    "boom",
		"nilv": nil,
		"m":    map[string]any{"nested": true},
		"esc":  strings.Repeat("a", 20) + "\"quoted\"\u007f",
	}
	for k, want := range checks {
		got, ok := payload[k]
		if !ok {
			t.Fatalf("missing field %q in %v", k, payload)
		}
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			t.Fatalf("field %q = %s, want %s", k, gotJSON, wantJSON)
		}
	}
}

func TestJSONSinkBufferReuse(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONSink(&buf)
	for i := 0; i < 100; i++ {
		sink.Write(LevelInfo, "m", map[string]any{"i": i, "big": strings.Repeat("x", 2000)})
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 100 {
		t.Fatalf("got %d lines, want 100", len(lines))
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[99]), &last); err != nil {
		t.Fatal(err)
	}
	if last["i"] != 99.0 || last["big"] != strings.Repeat("x", 2000) {
		t.Fatalf("recycled buffer corrupted: %v", last)
	}
}

func TestJSONSinkNilSafety(t *testing.T) {
	var sink *JSONSink
	sink.Write(LevelInfo, "no panic", nil) // must not panic
	NewJSONSink(nil).Write(LevelInfo, "no panic", nil)
	var buf bytes.Buffer
	NewJSONSink(&buf).Write(LevelInfo, "", nil)
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestJSONSinkConcurrent(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	w := lockedWriter{mu: &mu, buf: &buf}
	sink := NewJSONSink(w)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				sink.Write(LevelInfo, "concurrent", map[string]any{
					"g":    g,
					"i":    i,
					"fill": strings.Repeat("z", 32),
				})
			}
		}(g)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1600 {
		t.Fatalf("got %d lines, want 1600", len(lines))
	}
	for _, ln := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ln), &payload); err != nil {
			t.Fatalf("corrupt line under concurrency: %q", ln)
		}
	}
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

var _ io.Writer = lockedWriter{}
