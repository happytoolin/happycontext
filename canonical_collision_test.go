package hc

// Canonical-key collision policy (T5 decision, logrus precedent):
// user fields named "message", "time", or "level" collide with the
// canonical envelope members the encoder appends around the fields,
// which used to yield duplicate JSON keys that parsers disagree on.
// The encoder now renames colliding user keys to "fields.message",
// "fields.time", "fields.level" on the wire (record.go aliasKey) —
// the logrus Fields.*-prefix convention. The rename is wire-only:
// Record.Fields() and Lookup keep returning the user's original key.
//
// These tests pin the decision; the dedupe fuzz targets pin the same
// policy across the width boundaries and the fuzzer's key space.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestCanonicalCollisionRename: user "message"/"time"/"level" appear
// on the wire as fields.*, and the canonical envelope members stay
// unique.
func TestCanonicalCollisionRename(t *testing.T) {
	cases := []struct {
		userKey string
		wireKey string
		value   any
	}{
		{"message", "fields.message", "user-msg"},
		{"time", "fields.time", "user-time"},
		{"level", "fields.level", "user-level"},
	}
	for _, c := range cases {
		t.Run(c.userKey, func(t *testing.T) {
			r := recOf(LevelInfo, "m", fieldOf(c.userKey, c.value))
			line := string(r.Encoded())

			// The user value travels under the renamed fields.* key.
			var s string
			if raw := memberValue(t, line, c.wireKey); raw == nil {
				t.Fatalf("wire lacks %q: %s", c.wireKey, line)
			} else if err := json.Unmarshal(raw, &s); err != nil || s != c.value {
				t.Fatalf("%s = %s, want %q", c.wireKey, raw, c.value)
			}
			// The raw key appears exactly once — the canonical envelope
			// member, carrying the canonical value (no duplicate keys).
			if n := strings.Count(line, `"`+c.userKey+`":`); n != 1 {
				t.Fatalf("raw key %q appears %d times on the wire: %s", c.userKey, n, line)
			}
			wantCanonical := map[string]string{
				"message": "m",
				"time":    r.completedAt.Format(time.RFC3339), // recOf's fixed clock
				"level":   "info",
			}[c.userKey]
			if raw := memberValue(t, line, c.userKey); raw == nil || string(raw) != `"`+wantCanonical+`"` {
				t.Fatalf("canonical %q = %s, want %q: %s", c.userKey, raw, wantCanonical, line)
			}
		})
	}
}

// memberValue returns the raw value of the FIRST member with the key
// (nil when absent).
func memberValue(t *testing.T, line, key string) json.RawMessage {
	t.Helper()
	_, members := decodeLineStrict(t, []byte(line))
	for _, m := range members {
		if m.key == key {
			return m.val
		}
	}
	return nil
}

// TestCanonicalCollisionEnvelopeIntact: the canonical message/time/
// level values are untouched by a same-named user field, and the wire
// parses without duplicate keys.
func TestCanonicalCollisionEnvelopeIntact(t *testing.T) {
	ts := NewTestSink()
	rt := MustCompile(Config{Sink: ts, SamplingRate: 1})
	op := Start(context.Background(), rt, OperationStart{Domain: DomainJob, Name: "collide"})
	Add(op.Context(), "message", "user-message")
	Add(op.Context(), "time", "user-time")
	Add(op.Context(), "level", "user-level")
	op.End(nil)

	ev := ts.Events()[0]
	if ev.Message() != DefaultOperationMessage {
		t.Fatalf("message = %q, want the canonical default", ev.Message())
	}
	// the parsed map must have exactly one of each envelope key: parse
	// the captured record's canonical line
	line := capturedLine(t, ev)
	var payload map[string]any
	if err := json.Unmarshal(line, &payload); err != nil {
		t.Fatalf("line not parseable: %v (%s)", err, line)
	}
	for _, k := range []string{"level", "time", "message"} {
		if _, ok := payload[k]; !ok {
			t.Fatalf("canonical %q missing from the parsed payload", k)
		}
	}
	if payload["message"] != DefaultOperationMessage {
		t.Fatalf("parsed message = %v, want the canonical default", payload["message"])
	}
	if payload["fields.message"] != "user-message" {
		t.Fatalf("fields.message = %v", payload["fields.message"])
	}
}

// TestCanonicalCollisionLWW: a user "message" and a user
// "fields.message" fold into ONE member — the encoder dedupes over
// the aliased keys (last write wins at its last occurrence).
func TestCanonicalCollisionLWW(t *testing.T) {
	// fields.message first, message later → message wins
	r := recOf(
		LevelInfo, "m",
		fieldOf("fields.message", "first"),
		fieldOf("message", "second"),
	)
	line := string(r.Encoded())
	if strings.Count(line, `"fields.message"`) != 1 {
		t.Fatalf("fields.message emitted %d times: %s", strings.Count(line, `"fields.message"`), line)
	}
	if !strings.Contains(line, `"fields.message":"second"`) {
		t.Fatalf("last write did not win: %s", line)
	}

	// message first, fields.message later → fields.message wins
	r = recOf(
		LevelInfo, "m",
		fieldOf("message", "first"),
		fieldOf("fields.message", "second"),
	)
	line = string(r.Encoded())
	if strings.Count(line, `"fields.message"`) != 1 || !strings.Contains(line, `"fields.message":"second"`) {
		t.Fatalf("last write did not win: %s", line)
	}
}

// TestCanonicalCollisionLookupKeepsRawKey: the rename is wire-only —
// the typed view keeps the user's original key.
func TestCanonicalCollisionLookupKeepsRawKey(t *testing.T) {
	r := recOf(LevelInfo, "m", fieldOf("message", "user-msg"))
	if v, ok := r.Lookup("message"); !ok || v != "user-msg" {
		t.Fatalf("Lookup(message) = %v %v", v, ok)
	}
	fields := r.Fields()
	if len(fields) != 1 || fields[0].Key() != "message" {
		t.Fatalf("Fields() = %v", fields)
	}
}

// capturedLine re-encodes a captured event for parsing (TestSink does
// not retain the wire bytes).
func capturedLine(t *testing.T, ev CapturedEvent) []byte {
	t.Helper()
	r := &Record{
		level: ev.Level(), msg: ev.Message(), fields: ev.Fields(),
		completedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local),
	}
	return r.Encoded()
}
