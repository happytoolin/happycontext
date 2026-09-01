package hc

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func recOf(level Level, msg string, fields ...Field) *Record {
	return &Record{level: level, msg: msg, fields: fields, completedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)}
}

func TestRecordFieldsZeroCopy(t *testing.T) {
	fields := []Field{fieldOf("a", 1), fieldOf("b", 2)}
	r := recOf(LevelInfo, "m", fields...)
	got := r.Fields()
	if &got[0] != &fields[0] {
		t.Fatal("Fields() copied the slice")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestRecordLookup(t *testing.T) {
	r := recOf(LevelInfo, "m", fieldOf("k", 1), fieldOf("k", 2), fieldOf("s", "x"))
	if v, ok := r.Lookup("k"); !ok || v.(int64) != 2 {
		t.Fatalf("Lookup(k) = %v %v, want last value 2", v, ok)
	}
	if v, ok := r.Lookup("s"); !ok || v.(string) != "x" {
		t.Fatalf("Lookup(s) = %v %v", v, ok)
	}
	if _, ok := r.Lookup("missing"); ok {
		t.Fatal("missing key found")
	}
}

func TestRecordEncodedDedupLastWriteWins(t *testing.T) {
	r := recOf(
		LevelInfo, "m",
		fieldOf("k", 1),
		fieldOf("other", true),
		fieldOf("k", 2),
	)
	line := string(r.Encoded())
	if strings.Count(line, `"k":`) != 1 {
		t.Fatalf("duplicate key emitted more than once: %s", line)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["k"] != float64(2) {
		t.Fatalf("k = %v, want 2 (last write)", payload["k"])
	}
}

func TestRecordEncodedWideDedupePath(t *testing.T) {
	fields := make([]Field, 0, 40)
	for i := 0; i < 30; i++ {
		fields = append(fields, fieldOf(strings.Repeat("a", i+1), i))
	}
	fields = append(fields, fieldOf(strings.Repeat("a", 1), 999)) // dup of first key
	r := recOf(LevelInfo, "m", fields...)
	line := string(r.Encoded())
	if strings.Count(line, `"aa":`) != 1 {
		t.Fatalf("wide-path duplicate emitted more than once")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["a"] != float64(999) {
		t.Fatalf("a = %v, want last write 999", payload["a"])
	}
	if len(payload) != 33 { // 30 unique keys + level + time + message
		t.Fatalf("payload size = %d", len(payload))
	}
}

func TestRecordEncodedShape(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r := &Record{
		level:       LevelError,
		msg:         "job failed",
		fields:      []Field{fieldOf("op.code", 500), fieldOf("dur", 1500*time.Millisecond)},
		completedAt: now,
	}
	line := r.Encoded()
	if !bytes.HasSuffix(line, []byte("}\n")) {
		t.Fatalf("line must end with }\\n: %q", line)
	}
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["level"] != "error" {
		t.Fatalf("level = %v", payload["level"])
	}
	if payload["message"] != "job failed" {
		t.Fatalf("message = %v", payload["message"])
	}
	if ts, ok := payload["time"].(string); !ok || ts != "2026-09-01T10:00:00Z" {
		t.Fatalf("time = %v", payload["time"])
	}
	if payload["dur"] != 1500.0 { // float ms
		t.Fatalf("dur = %v", payload["dur"])
	}
	// ordering: fields before time before message
	if !(strings.Index(string(line), `"op.code"`) < strings.Index(string(line), `"time"`) &&
		strings.Index(string(line), `"time"`) < strings.Index(string(line), `"message"`)) {
		t.Fatalf("field order wrong: %s", line)
	}
}

// TestRecordEncodedOnce pins amendment 6: concurrent callers race
// benignly and share one encoded buffer.
func TestRecordEncodedOnce(t *testing.T) {
	fields := make([]Field, 16)
	for i := range fields {
		fields[i] = fieldOf("k"+strings.Repeat("x", i), i)
	}
	r := recOf(LevelInfo, "m", fields...)

	var wg sync.WaitGroup
	first := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			first[i] = r.Encoded()
		}(i)
	}
	wg.Wait()
	for i := 1; i < 8; i++ {
		if &first[i][0] != &first[0][0] {
			t.Fatalf("caller %d got a different buffer: encoding ran twice", i)
		}
	}
}

func TestRecordFieldKindsOnWire(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r := recOf(
		LevelInfo, "m",
		fieldOf("s", "v"),
		fieldOf("i", -5),
		fieldOf("u8", uint8(7)),
		fieldOf("f", 2.5),
		fieldOf("b", true),
		fieldOf("t", now),
		fieldOf("d", time.Second),
		Field{key: "raw", kind: KindRaw, val: []byte(`{"pre":"encoded"}`)},
		fieldOf("e", errString("boom")),
		fieldOf("any", map[string]any{"n": 1}),
	)
	var payload map[string]any
	if err := json.Unmarshal(r.Encoded(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	checks := map[string]any{
		"s": "v", "i": -5.0, "u8": 7.0, "f": 2.5, "b": true,
		"t": now.Format(time.RFC3339), "d": 1000.0, "e": "boom",
		"any": map[string]any{"n": 1.0},
	}
	for k, want := range checks {
		got, ok := payload[k]
		if !ok {
			t.Fatalf("missing %q", k)
		}
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(got)
		if string(wj) != string(gj) {
			t.Fatalf("%q = %s, want %s", k, gj, wj)
		}
	}
	if raw, ok := payload["raw"].(map[string]any); !ok || raw["pre"] != "encoded" {
		t.Fatalf("raw field = %v", payload["raw"])
	}
}

type errString string

func (e errString) Error() string { return string(e) }
