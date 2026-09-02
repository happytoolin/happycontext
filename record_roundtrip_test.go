package hc

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// This file implements the record-level round-trip properties from the
// DST/fuzzing research (§7.1 field round-trip per kind, §7.2 encode
// determinism, §6.2 dedupe fuzz with an independent reference fold):
// Encoded() must produce a JSON line that parses back to exactly the
// record's resolved (last-write-wins) view, for every field kind, at
// every width, under adversarial duplicate structure.

// decodeLineStrict parses one canonical line and returns both the
// last-wins map (Go's json semantics) and the ordered raw members.
type rawMember struct {
	key string
	val json.RawMessage
}

func decodeLineStrict(t *testing.T, line []byte) (map[string]json.RawMessage, []rawMember) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		t.Fatalf("Encoded() is not valid JSON: %v (%q)", err, line)
	}
	// ordered re-walk for order assertions
	var ordered []rawMember
	dec2 := json.NewDecoder(bytes.NewReader(line))
	dec2.Token() // {
	for {
		tok, err := dec2.Token()
		if err != nil {
			break
		}
		if key, ok := tok.(string); ok {
			var raw json.RawMessage
			if err := dec2.Decode(&raw); err != nil {
				t.Fatalf("member %q: %v", key, err)
			}
			ordered = append(ordered, rawMember{key, raw})
		}
		if tok == json.Delim('}') {
			break
		}
	}
	return top, ordered
}

// TestEncodedRoundTripAllKinds walks every field kind through
// Encoded() → parse → compare, on both friendly and adversarial values.
func TestEncodedRoundTripAllKinds(t *testing.T) {
	utc := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	ist := time.Date(2026, 9, 1, 15, 30, 0, 0, time.FixedZone("IST", 5*3600+30*60))

	cases := []struct {
		key     string
		field   Field
		check   func(t *testing.T, raw json.RawMessage)
		skipOrd bool // kinds whose wire form is embedded/structured
	}{
		{"s", fieldOf("s", "v"), rawStringIs("v"), false},
		{"s_quote", fieldOf("s_quote", `she said "hi"`), rawStringIs(`she said "hi"`), false},
		{"s_nl", fieldOf("s_nl", "a\nb\tc"), rawStringIs("a\nb\tc"), false},
		{"s_uni", fieldOf("s_uni", "héllo ☃ 🍜"), rawStringIs("héllo ☃ 🍜"), false},
		{"s_ctl", fieldOf("s_ctl", "x\x00\x7fy"), rawStringIs("x\x00\x7fy"), false},
		{"i", fieldOf("i", -42), rawNumberIs("-42"), false},
		{"i_min", fieldOf("i_min", int64(math.MinInt64)), rawNumberIs(strconv.FormatInt(math.MinInt64, 10)), false},
		{"i_max", fieldOf("i_max", int64(math.MaxInt64)), rawNumberIs(strconv.FormatInt(math.MaxInt64, 10)), false},
		{"u_max", fieldOf("u_max", uint64(math.MaxUint64)), rawNumberIs(strconv.FormatUint(math.MaxUint64, 10)), false},
		{"f", fieldOf("f", 2.5), rawNumberIs("2.5"), false},
		{"f_neg0", fieldOf("f_neg0", math.Copysign(0, -1)), rawNumberIs("-0"), false},
		{"f_big", fieldOf("f_big", 1e21), rawNumberIs("1e+21"), false},
		{"f32", fieldOf("f32", float32(0.1)), rawNumberIs("0.1"), false},
		{"f_nan", fieldOf("f_nan", math.NaN()), rawStringIs("NaN"), false},
		{"f_pinf", fieldOf("f_pinf", math.Inf(1)), rawStringIs("+Inf"), false},
		{"b", fieldOf("b", true), rawIs("true"), false},
		{"t_utc", fieldOf("t_utc", utc), rawStringIs(utc.Format(time.RFC3339)), false},
		{"t_zone", fieldOf("t_zone", ist), rawStringIs(ist.Format(time.RFC3339)), false},
		{"d", fieldOf("d", 1500*time.Millisecond), rawNumberIs("1500"), false},
		{"d_frac", fieldOf("d_frac", 123456789*time.Nanosecond), rawNumberIs("123.456789"), false},
		{"d_neg", fieldOf("d_neg", -time.Second), rawNumberIs("-1000"), false},
		{"e", fieldOf("e", errString("boom")), rawStringIs("boom"), false},
		{"any_obj", fieldAny("any_obj", map[string]any{"n": 1}), rawIs(`{"n":1}`), true},
		{"any_nil", fieldAny("any_nil", nil), rawIs("null"), false},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			r := recOf(LevelInfo, "m", c.field)
			got, ordered := decodeLineStrict(t, r.Encoded())
			raw, ok := got[c.key]
			if !ok {
				t.Fatalf("key %q missing from %v", c.key, got)
			}
			c.check(t, raw)
			if !c.skipOrd {
				found := false
				for _, m := range ordered {
					if m.key == c.key {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("key %q not in ordered members", c.key)
				}
			}
		})
	}

	// raw fields are embedded verbatim
	r := recOf(LevelInfo, "m", Field{key: "raw", kind: KindRaw, val: []byte(`{"pre":true}`)})
	got, _ := decodeLineStrict(t, r.Encoded())
	if string(got["raw"]) != `{"pre":true}` {
		t.Fatalf("raw field = %s", got["raw"])
	}
}

func rawStringIs(want string) func(*testing.T, json.RawMessage) {
	return func(t *testing.T, raw json.RawMessage) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("not a string: %s (%v)", raw, err)
		}
		if s != want {
			t.Fatalf("string = %q, want %q", s, want)
		}
	}
}

func rawNumberIs(want string) func(*testing.T, json.RawMessage) {
	return func(t *testing.T, raw json.RawMessage) {
		if n := strings.TrimPrefix(string(raw), `"`); n != want {
			t.Fatalf("number = %s, want %s", raw, want)
		}
	}
}

func rawIs(want string) func(*testing.T, json.RawMessage) {
	return func(t *testing.T, raw json.RawMessage) {
		if string(raw) != want {
			t.Fatalf("value = %s, want %s", raw, want)
		}
	}
}

// TestEncodedRoundTripProperty is the generated counterpart: PCG-seeded
// random field sets (strings with injected specials, ints across the
// 2^53 boundary, floats, durations) must survive the encode → parse →
// compare cycle exactly.
func TestEncodedRoundTripProperty(t *testing.T) {
	const total = 20_000
	rng := rand.New(rand.NewPCG(0x600D5eed, 0x51EE5eed))

	specials := []byte{0x00, 0x09, 0x0a, '"', '\\', 0x7f, 0x80, 0xff}
	for i := 0; i < total; i++ {
		width := 1 + rng.IntN(8)
		fields := make([]Field, width)
		for w := range fields {
			key := "k" + strconv.Itoa(rng.IntN(6))
			switch rng.IntN(4) {
			case 0:
				b := make([]byte, rng.IntN(24))
				for j := range b {
					if rng.IntN(8) == 0 {
						b[j] = specials[rng.IntN(len(specials))]
					} else {
						b[j] = byte(0x20 + rng.IntN(0x5f))
					}
				}
				fields[w] = fieldOf(key, string(b))
			case 1:
				fields[w] = fieldOf(key, rng.Int64())
			case 2:
				fields[w] = fieldOf(key, rng.Float64())
			case 3:
				fields[w] = fieldOf(key, time.Duration(rng.Int64())*time.Millisecond)
			}
		}
		r := recOf(LevelInfo, "prop", fields...)
		got, _ := decodeLineStrict(t, r.Encoded())

		// reference fold: last write wins per key, forward order
		var wantKeys []string
		wantVals := map[string]Field{}
		for _, f := range fields {
			if _, seen := wantVals[f.key]; !seen {
				wantKeys = append(wantKeys, f.key)
			}
			wantVals[f.key] = f
		}
		if len(got) != len(wantKeys)+3 { // + level, time, message
			t.Fatalf("i=%d: %d keys on wire, want %d (+3 canonical)", i, len(got), len(wantKeys))
		}
		for k, wf := range wantVals {
			raw, ok := got[k]
			if !ok {
				t.Fatalf("i=%d: key %q missing", i, k)
			}
			var s string
			var num int64
			var fnum float64
			switch wf.kind {
			case KindString:
				if err := json.Unmarshal(raw, &s); err != nil || (!utf8.ValidString(wf.str) && err != nil) {
					t.Fatalf("i=%d %q: %v", i, k, err)
				}
				if utf8.ValidString(wf.str) && s != wf.str {
					t.Fatalf("i=%d %q: string round-trip %q != %q", i, k, s, wf.str)
				}
			case KindInt:
				if err := json.Unmarshal(raw, &num); err != nil || num != wf.num {
					t.Fatalf("i=%d %q: int round-trip %d != %d (%s)", i, k, num, wf.num, raw)
				}
			case KindFloat:
				if err := json.Unmarshal(raw, &fnum); err != nil || fnum != wf.f {
					t.Fatalf("i=%d %q: float round-trip %v != %v", i, k, fnum, wf.f)
				}
			case KindDuration:
				var ms float64
				if err := json.Unmarshal(raw, &ms); err != nil || ms != float64(wf.num)/float64(time.Millisecond) {
					t.Fatalf("i=%d %q: duration round-trip %v != %v", i, k, ms, float64(wf.num)/float64(time.Millisecond))
				}
			}
		}
	}
}

// TestEncodedDeterminism: two records built from equal inputs encode to
// equal bytes — encoding is a pure function of (level, msg, fields,
// completedAt) (dst-research §7.2).
func TestEncodedDeterminism(t *testing.T) {
	build := func() *Record {
		return recOf(LevelWarn, "same",
			fieldOf("a", 1), fieldOf("b", "two"), fieldOf("a", 3),
			fieldOf("f", 0.1), fieldOf("d", time.Second))
	}
	if !bytes.Equal(build().Encoded(), build().Encoded()) {
		t.Fatal("two equal records encoded differently")
	}
}

// FuzzRecordEncodedDedupe is the dedupe-boundary fuzz target
// (dst-research §6.2): generated field sequences with dense duplicates
// and canonical-key collisions must produce a line where every user key
// appears exactly once with its last value, in last-occurrence order —
// checked against an independent reference fold, not the encoder's own
// dedupe code.
//
// Canonical-key collision follows the logrus rename policy (record.go
// aliasKey, T5 decision): user fields named "level"/"time"/"message"
// are emitted as "fields.level"/"fields.time"/"fields.message", so the
// envelope keys — member 0 level, last two members time and message —
// stay unique on the wire. The reference fold aliases the keys the
// same way (a user "message" and a user "fields.message" fold into one
// member).
func FuzzRecordEncodedDedupe(f *testing.F) {
	f.Add(uint8(0), []byte{0, 1, 2, 0, 1}, []byte{7, 7, 7})
	f.Add(uint8(24), []byte{0, 1, 2, 3, 0, 1, 2, 3}, []byte{1, 2})
	f.Add(uint8(25), []byte{0, 0, 0, 0}, []byte{1, 2, 3})
	f.Add(uint8(31), []byte{0, 1, 2, 3, 4, 5, 6, 7}, []byte{0})
	f.Add(uint8(32), []byte{0}, []byte{1}) // seenArr → map handoff
	f.Add(uint8(64), []byte{3, 3, 3}, []byte{9})
	f.Add(uint8(5), []byte{4, 4, 4}, []byte{1}) // all keys are "message"
	f.Add(uint8(5), []byte{5, 5}, []byte{1})    // all keys are "time"
	f.Add(uint8(5), []byte{6, 6}, []byte{1})    // all keys are "level"
	f.Fuzz(func(t *testing.T, widthRaw uint8, keySeed []byte, valSeed []byte) {
		width := int(widthRaw) % 80
		if len(keySeed) == 0 {
			return
		}
		alphabet := []string{"a", "bb", "ccc", "message", "time", "level", "zz"}
		fields := make([]Field, width)
		for i := range fields {
			key := alphabet[int(keySeed[i%len(keySeed)])%len(alphabet)]
			val := int64(0)
			if len(valSeed) > 0 {
				val = int64(valSeed[i%len(valSeed)])
			}
			fields[i] = fieldOf(key, val)
		}

		r := recOf(LevelInfo, "m", fields...)
		got, ordered := decodeLineStrict(t, r.Encoded())

		// reference fold (independent of appendDedupedFields)
		type lastWrite struct {
			val int64
			pos int // last-occurrence position, for order
		}
		lww := map[string]lastWrite{}
		for i, fl := range fields {
			lww[aliasedFieldKey(fl.key)] = lastWrite{fl.num, i}
		}
		var wantOrder []string
		{
			type kv struct {
				k   string
				pos int
			}
			var kvs []kv
			for k, lw := range lww {
				kvs = append(kvs, kv{k, lw.pos})
			}
			// sort by last-occurrence position ascending = emission order
			for i := 1; i < len(kvs); i++ {
				for j := i; j > 0 && kvs[j].pos < kvs[j-1].pos; j-- {
					kvs[j], kvs[j-1] = kvs[j-1], kvs[j]
				}
			}
			for _, kv := range kvs {
				wantOrder = append(wantOrder, kv.k)
			}
		}

		// 1. every aliased user key exactly once on the wire. The
		// envelope keys (level/time/message) never appear among the user
		// members — collisions were renamed to fields.* — so every fold
		// key must be a user member.
		for k := range lww {
			if k == "level" || k == "time" || k == "message" {
				continue // envelope members, checked by check 3
			}
			n := 0
			for _, m := range ordered {
				if m.key == k {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("key %q emitted %d times (width %d): %s", k, n, width, r.Encoded())
			}
			var v int64
			if err := json.Unmarshal(got[k], &v); err != nil || v != lww[k].val {
				t.Fatalf("key %q = %s, want last value %d", k, got[k], lww[k].val)
			}
		}

		// 1b. colliding user keys never appear under their raw names in
		// the user span, and their fields.* copies are covered by the
		// fold loop above.
		for _, m := range ordered {
			switch m.key {
			case "level", "time", "message":
				// envelope members only — the user span excludes them
				// by construction (check 3)
			default:
				if _, user := lww[m.key]; !user {
					t.Fatalf("wire member %q is not a fold key (line %s)", m.key, r.Encoded())
				}
			}
		}

		// 2. emission order == last-occurrence order. The envelope keys
		// occupy fixed slots (member 0 is the canonical level; the last two
		// members are the canonical time and message), so the user span is
		// strictly between them regardless of collisions.
		var gotOrder []string
		for _, m := range ordered {
			gotOrder = append(gotOrder, m.key)
		}
		userGot := gotOrder[1 : len(gotOrder)-2]
		for i := range wantOrder {
			if userGot[i] != wantOrder[i] {
				t.Fatalf("order mismatch at %d: got %v want %v (line %s)", i, userGot, wantOrder, r.Encoded())
			}
		}

		// 3. canonical envelope pin: level/time/message appear exactly
		// once each as envelope members regardless of colliding user
		// fields — the rename guarantees the wire is duplicate-free.
		for _, canon := range []string{"level", "time", "message"} {
			n := 0
			for _, m := range ordered {
				if m.key == canon {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("canonical key %q appears %d times, want exactly 1 "+
					"(rename policy; line %s)", canon, n, r.Encoded())
			}
		}
	})
}
