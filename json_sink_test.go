package hc

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJSONSinkWritesEncodedRecord(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONSink(&buf)
	rec := recOf(LevelError, "job failed", fieldOf("op.code", 500), fieldOf("dur", time.Second))
	sink.Write(context.Background(), rec)

	line := buf.Bytes()
	if !bytes.HasSuffix(line, []byte("}\n")) || bytes.Count(line, []byte("\n")) != 1 {
		t.Fatalf("one line expected, got %q", line)
	}
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["level"] != "error" || payload["message"] != "job failed" || payload["op.code"] != 500.0 {
		t.Fatalf("payload = %v", payload)
	}
	if _, err := time.Parse(time.RFC3339, payload["time"].(string)); err != nil {
		t.Fatalf("time not RFC3339: %v", payload["time"])
	}
}

func TestJSONSinkEncodeOnceReuse(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	s1 := NewJSONSink(&buf1)
	s2 := NewJSONSink(&buf2)
	rec := recOf(LevelInfo, "m", fieldOf("k", 1))
	s1.Write(context.Background(), rec)
	s2.Write(context.Background(), rec)
	if !bytes.Equal(buf1.Bytes(), buf2.Bytes()) {
		t.Fatal("two sinks saw different encodings of the same record")
	}
}

func TestJSONSinkDefaultLevelMessage(t *testing.T) {
	var buf bytes.Buffer
	NewJSONSink(&buf).Write(context.Background(), recOf(Level(99), "", fieldOf("x", 1)))
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["level"] != "info" {
		t.Fatalf("unknown level rendered %v", payload["level"])
	}
}

func TestJSONSinkFloatEdgesOnWire(t *testing.T) {
	var buf bytes.Buffer
	rec := recOf(
		LevelInfo, "f",
		fieldOf("nan", math.NaN()),
		fieldOf("pinf", math.Inf(1)),
		fieldOf("ninf", math.Inf(-1)),
		fieldOf("tiny", 1e-7),
	)
	NewJSONSink(&buf).Write(context.Background(), rec)
	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["nan"] != "NaN" || payload["pinf"] != "+Inf" || payload["ninf"] != "-Inf" {
		t.Fatalf("float edges = %v %v %v", payload["nan"], payload["pinf"], payload["ninf"])
	}
	if payload["tiny"] != 1e-7 {
		t.Fatalf("tiny = %v", payload["tiny"])
	}
}

func TestJSONSinkEscapesAndUnicode(t *testing.T) {
	var buf bytes.Buffer
	rec := recOf(
		LevelInfo, "m",
		fieldOf("url", "/search?q="+strings.Repeat("héllo☃", 8)),
		fieldOf("agent", `Mozilla/5.0 ("quote" back\slash)`),
		fieldOf("nul", "x\x00y"),
		fieldOf("del", "d\x7f"),
	)
	NewJSONSink(&buf).Write(context.Background(), rec)
	line := buf.String()
	if strings.Contains(line, "\x00") || strings.Contains(line, "\x7f") {
		t.Fatal("control bytes leaked unescaped")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload["nul"] != "x\x00y" || payload["del"] != "d\x7f" {
		t.Fatalf("round-trip broke: %v %v", payload["nul"], payload["del"])
	}
}

func TestJSONSinkConcurrency(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	w := lockedTestWriter{mu: &mu, buf: &buf}
	sink := NewJSONSink(w)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				rec := recOf(LevelInfo, "concurrent", fieldOf("g", g), fieldOf("i", i))
				sink.Write(context.Background(), rec)
			}
		}(g)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 800 {
		t.Fatalf("lines = %d", len(lines))
	}
	for _, ln := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ln), &payload); err != nil {
			t.Fatalf("corrupt line: %q", ln)
		}
	}
}

func TestJSONSinkNilSafety(t *testing.T) {
	var sink *JSONSink
	sink.Write(context.Background(), recOf(LevelInfo, "m")) // no panic
	NewJSONSink(nil).Write(context.Background(), recOf(LevelInfo, "m"))
}

type lockedTestWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w lockedTestWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func TestTestSinkCapture(t *testing.T) {
	ts := NewTestSink()
	rec := recOf(LevelWarn, "m", fieldOf("i", 1), fieldOf("s", "x"), fieldOf("any", map[string]any{"k": 1}))
	ts.Write(context.Background(), rec)

	events := ts.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	ev := events[0]
	if ev.Level() != LevelWarn || ev.Message() != "m" {
		t.Fatalf("event = %v %v", ev.Level(), ev.Message())
	}
	if v, ok := ev.Lookup("i"); !ok || v.(int64) != 1 {
		t.Fatalf("i = %v", v)
	}
	if v, _ := ev.Lookup("any"); v.(map[string]any)["k"] != 1 {
		// map values are deep-copied on capture; originals may mutate freely
		t.Fatalf("any = %v", v)
	}
}

func TestTestSinkCopiesMutableValues(t *testing.T) {
	ts := NewTestSink()
	shared := map[string]any{"k": 1}
	ts.Write(context.Background(), recOf(LevelInfo, "m", fieldOf("m", shared)))
	shared["k"] = 999 // mutate after capture
	ev := ts.Events()[0]
	v, _ := ev.Lookup("m")
	if v.(map[string]any)["k"] != 1 {
		t.Fatal("capture retained a reference to caller state")
	}
	ts.Reset()
	if len(ts.Events()) != 0 {
		t.Fatal("Reset did not clear")
	}
}
