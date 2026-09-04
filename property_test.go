package hc

// Property-based tests: round-trip, sampling invariants, pool safety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// rtFieldValue generates one typed value for a kind. The value space
// per kind includes hostile edges (int64 extremes, float subnormals,
// invalid UTF-8, exotic zones).
func rtFieldValue(rng *rand.Rand, kind FieldKind) any {
	edge := func(p float64) bool { return rng.Float64() < p }
	switch kind {
	case KindString:
		if edge(0.15) {
			// invalid UTF-8: broken sequences, lone continuation bytes
			n := 1 + rng.IntN(12)
			b := make([]byte, n)
			for i := range b {
				b[i] = []byte{0x80, 0xff, 0xc0, 0xfe, 0x22, 0x5c}[rng.IntN(6)]
			}
			return string(b)
		}
		n := rng.IntN(24)
		b := make([]byte, n)
		for i := range b {
			switch rng.IntN(10) {
			case 0:
				b[i] = byte(rng.IntN(0x20)) // control bytes
			case 1:
				b[i] = '"'
			case 2:
				b[i] = '\\'
			case 3:
				b[i] = 0x7f
			default:
				b[i] = byte(0x20 + rng.IntN(0x5f))
			}
		}
		if edge(0.2) {
			return string(b) + " héllo ☃ 🍜"
		}
		return string(b)
	case KindInt:
		switch rng.IntN(6) {
		case 0:
			return int64(math.MinInt64)
		case 1:
			return int64(math.MaxInt64)
		case 2:
			return int64(1) << 62
		case 3:
			return -(int64(1) << 62)
		case 4:
			return int64(int8(rng.Uint64()))
		default:
			return int64(rng.Uint64())
		}
	case KindUint:
		if edge(0.3) {
			return uint64(math.MaxUint64)
		}
		return uint64(rng.Uint64())
	case KindFloat:
		switch rng.IntN(8) {
		case 0:
			return 0.0
		case 1:
			return math.Copysign(0, -1) // -0 survives bitwise
		case 2:
			return math.SmallestNonzeroFloat64
		case 3:
			return math.MaxFloat64
		case 4:
			return float64(1 << 53)
		case 5:
			return math.NaN() // quoted-string policy
		case 6:
			return math.Inf(int(rng.Uint64()%2)*2 - 1)
		default:
			return rng.Float64()*2e21 - 1e21
		}
	case KindFloat32:
		switch rng.IntN(5) {
		case 0:
			return float32(0.1) // the not-widened gate
		case 1:
			return float32(math.SmallestNonzeroFloat32)
		case 2:
			return float32(math.MaxFloat32)
		case 3:
			return float32(1) / 3
		default:
			return float32(rng.Float64())
		}
	case KindBool:
		return rng.Uint64()&1 == 0
	case KindTime:
		zones := []*time.Location{
			time.UTC,
			time.FixedZone("+14", 14*3600),
			time.FixedZone("-12", -12*3600),
			time.FixedZone("+05:30", 5*3600+1800),
		}
		year := 1 + rng.IntN(9999)
		return time.Date(year, time.Month(1+rng.IntN(12)), 1+rng.IntN(28),
			0+rng.IntN(24), 0+rng.IntN(60), 0+rng.IntN(60), rng.IntN(1e9),
			zones[rng.IntN(len(zones))])
	case KindDuration:
		switch rng.IntN(4) {
		case 0:
			return -time.Duration(rng.Uint64()) // negative
		case 1:
			return time.Duration(rng.IntN(1e6)) * time.Nanosecond
		default:
			return time.Duration(int64(rng.Uint64())) // full range
		}
	case KindErr:
		switch rng.IntN(3) {
		case 0:
			return fmt.Errorf("boom-%d", rng.IntN(1e6))
		case 1:
			return errString("plain")
		default:
			return fmt.Errorf("wrapped: %w", errors.New("inner"))
		}
	case KindRaw:
		// raw embeds verbatim: the blobs must stay valid JSON or the
		// whole line breaks (the caller's contract).
		switch rng.IntN(5) {
		case 0:
			return []byte(fmt.Sprintf(`{"n":%d}`, rng.IntN(1e6)))
		case 1:
			return []byte(`{"nested":{"deep":[1,true,null]}}`)
		case 2:
			return []byte(fmt.Sprintf(`{"s":"v%d"}`, rng.IntN(100)))
		case 3:
			return []byte(`[1,2,3]`)
		default:
			return []byte(`null`)
		}
	case KindAny:
		switch rng.IntN(6) {
		case 0:
			return nil
		case 1:
			return "str"
		case 2:
			return int64(rng.IntN(1000))
		case 3:
			return []any{int64(1), "x", true, nil}
		case 4:
			return map[string]any{"a": int64(1), "b": "two", "c": []any{false}}
		default:
			return map[string]any{"nested": map[string]any{"k": fmt.Sprintf("v%d", rng.IntN(10))}}
		}
	default:
		return "unreachable"
	}
}

// rtWireCheck checks one encoded member against the modeled value via
// checkFieldWire, with the special float policy (NaN/±Inf are quoted
// strings) and invalid-UTF-8 handled before the generic checker.
func rtWireCheck(t *testing.T, kind FieldKind, val any, raw []byte) {
	t.Helper()
	f := fieldOf("k", val)
	switch kind {
	case KindFloat:
		fv := val.(float64)
		if math.IsNaN(fv) || math.IsInf(fv, 0) {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("NaN/Inf float not quoted: %s (%v)", raw, err)
			}
			want := map[bool]string{true: "NaN", false: ""}[math.IsNaN(fv)]
			if want == "" {
				if math.IsInf(fv, 1) {
					want = "+Inf"
				} else {
					want = "-Inf"
				}
			}
			if s != want {
				t.Fatalf("special float wire %q, want %q", s, want)
			}
			return
		}
	case KindString:
		if s, ok := val.(string); ok && !utf8.ValidString(s) {
			// invalid UTF-8: parseability only (mapping pinned by the
			// hcjson suites)
			var decoded string
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("invalid-UTF-8 string did not parse: %v (%s)", err, raw)
			}
			return
		}
	}
	if err := checkFieldWire(f, raw); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestFieldRoundTripProperty drives every kind through encode → parse
// → compare. Key names are unique per iteration so the fold never
// hides a kind.
func TestFieldRoundTripProperty(t *testing.T) {
	kinds := []struct {
		kind FieldKind
		name string
	}{
		{KindString, "string"},
		{KindInt, "int"},
		{KindUint, "uint"},
		{KindFloat, "float"},
		{KindFloat32, "float32"},
		{KindBool, "bool"},
		{KindTime, "time"},
		{KindDuration, "duration"},
		{KindErr, "err"},
		{KindRaw, "raw"},
		{KindAny, "any"},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			rng := rand.New(rand.NewPCG(uint64(k.kind)<<32|0x51EE5eed, 0x600D5eed))
			const n = 3000
			for i := range n {
				val := rtFieldValue(rng, k.kind)
				var r *Record
				if k.kind == KindRaw {
					blob := val.([]byte)
					r = recOf(LevelInfo, "m", Field{key: "k", kind: KindRaw, val: blob})
					line := r.Encoded()
					_, members := decodeLineStrict(t, line)
					if len(members) != 4 || members[1].key != "k" {
						t.Fatalf("iter %d: unexpected wire members %v", i, memberKeys(members))
					}
					// raw embeds verbatim: byte equality with the blob
					if !bytes.Equal(members[1].val, blob) {
						t.Fatalf("iter %d: raw wire %s != blob %s", i, members[1].val, blob)
					}
					continue
				}
				r = recOf(LevelInfo, "m", fieldOf("k", val))
				line := r.Encoded()
				_, members := decodeLineStrict(t, line)
				if len(members) != 4 { // level, k, time, message
					t.Fatalf("iter %d: wire members %v, want 4 (level k time message)", i, memberKeys(members))
				}
				if members[1].key != "k" {
					t.Fatalf("iter %d: member 1 = %q", i, members[1].key)
				}
				rtWireCheck(t, k.kind, val, members[1].val)
			}
		})
	}
}

// TestRecordEncodeDeterminismProperty pins encoding as a pure function
// of (level, msg, fields, completedAt): two separately built records
// with equal content encode to equal bytes (action plan P6).
func TestRecordEncodeDeterminismProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xD371E5eed, 0xD371E5eed))
	for i := range 2000 {
		width := 1 + rng.IntN(40)
		fields := make([]Field, width)
		for j := range fields {
			kind := FieldKind(1 + rng.IntN(11))
			fields[j] = fieldOf(fmt.Sprintf("k%d", j%7), rtFieldValue(rng, kind))
		}
		completedAt := time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)
		build := func() []byte {
			r := &Record{level: LevelInfo, msg: "m", fields: fields, completedAt: completedAt}
			return r.Encoded()
		}
		a, b := build(), build()
		if !bytes.Equal(a, b) {
			t.Fatalf("iter %d: equal records encoded differently", i)
		}
	}
}

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
	for i := range total {
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

// TestSamplingErrorBypassProperty drives generated failure and healthy
// programs through a drop-everything runtime (NeverSampler at rate 0):
// failures must still emit — the amendment-4 structural bypass — while
// healthy events drop. The model's error predicate (end error, panic,
// hc.Error, or a non-success outcome) is the oracle.
func TestSamplingErrorBypassProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5A9A1E5eed, 0x8A55E5eed))
	for i := range 3000 {
		buf := make([]byte, 2+rng.IntN(120))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate0
		m := buildModel(prog)
		outcome := m.outcome()

		sink := &lifeSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: 0, Sampler: NeverSampler()})
		op := Start(context.Background(), rt, OperationStart{Domain: prog.start, Name: "n"})
		executeProgramOn(prog, op)
		got := len(sink.events)
		want := 0
		if m.emitted(outcome) {
			want = 1
		}
		if got != want {
			t.Fatalf("iter %d: drop-everything runtime emitted %d events, want %d (outcome %s errOp=%v endErr=%v panicked=%v)",
				i, got, want, outcome, m.errOp, m.endErr != nil, m.endPanicked)
		}
	}
}

// TestSamplingRateBoundaryProperty pins the deterministic rate edges
// over generated healthy programs: rate 0 drops every healthy event,
// rate 1 keeps every one, and error events are emitted at both.
func TestSamplingRateBoundaryProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xB0A0A5eed, 0xD8A7A5eed))
	run := func(rate float64, prog lifeProgram) int {
		sink := &lifeSink{}
		rt := MustCompile(Config{Sink: sink, SamplingRate: rate})
		op := Start(context.Background(), rt, OperationStart{Domain: prog.start, Name: "n"})
		executeProgramOn(prog, op)
		return len(sink.events)
	}
	for i := range 1500 {
		buf := make([]byte, 2+rng.IntN(120))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate0 // runtime selection irrelevant: rate passed explicitly
		m := buildModel(prog)
		outcome := m.outcome()

		// Only programs that reach an End on a live runtime and stay
		// healthy belong here: failures (bypass test) and never-ended
		// streams (no event at any rate) are filtered out.
		if !m.ended || m.mode == modeNilRuntime || m.mode == modeNilSink || m.hasError(outcome) {
			continue
		}
		// healthy program: rate 0 drops, rate 1 keeps
		if got := run(0, prog); got != 0 {
			t.Fatalf("iter %d: healthy event survived rate 0", i)
		}
		if got := run(1, prog); got != 1 {
			t.Fatalf("iter %d: healthy event dropped at rate 1", i)
		}
	}
}

// chainCase describes one generated ChainSampler configuration.
type chainCase struct {
	hasErr    bool
	code      int
	status    int
	duration  time.Duration
	path      string
	keepErr   bool
	minDur    time.Duration
	prefixes  []string
	useSlower bool
	usePrefix bool
}

func chainReference(c chainCase) bool {
	// The reference union: each middleware keeps its predicate or
	// defers to the (dropping) base, so the composed decision is the OR
	// of the middlewares' predicates. KeepSlowerThan clamps negative
	// minimums to zero; KeepPathPrefix filters empty prefixes.
	keep := c.keepErr && (c.hasErr || c.code >= 500 || c.status >= 500)
	if c.useSlower {
		min := max(c.minDur, 0)
		if c.duration >= min {
			keep = true
		}
	}
	if c.usePrefix {
		for _, p := range c.prefixes {
			if p != "" && len(c.path) >= len(p) && c.path[:len(p)] == p {
				keep = true
			}
		}
	}
	return keep
}

// TestChainSamplerProperty compares ChainSampler's composed decision
// against the reference union formula over generated inputs.
func TestChainSamplerProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xC4A1A5eed, 0xE5EED5E))
	for i := range 5000 {
		c := chainCase{
			hasErr:    rng.Uint64()&1 == 0,
			code:      int(rng.IntN(700)),
			status:    int(rng.IntN(700)),
			duration:  time.Duration(rng.IntN(200)-50) * time.Millisecond,
			path:      []string{"", "/", "/api/v1/items", "/healthz", "/other"}[rng.IntN(5)],
			keepErr:   rng.Uint64()&1 == 0,
			minDur:    time.Duration(rng.IntN(100)-20) * time.Millisecond,
			useSlower: rng.Uint64()&1 == 0,
			usePrefix: rng.Uint64()&1 == 0,
		}
		switch rng.IntN(4) {
		case 0:
			c.prefixes = nil
		case 1:
			c.prefixes = []string{"/api"}
		case 2:
			c.prefixes = []string{"", "/healthz"}
		default:
			c.prefixes = []string{"/nope", "", "/api/v1"}
		}

		in := SampleInput{
			Domain: DomainHTTP, Operation: "GET /x", Outcome: OutcomeSuccess,
			Code: c.code, StatusCode: c.status, Duration: c.duration,
			Path: c.path, Level: LevelInfo, HasError: c.hasErr,
		}
		var middlewares []SamplerMiddleware
		if c.keepErr {
			middlewares = append(middlewares, KeepErrors())
		}
		if c.useSlower {
			middlewares = append(middlewares, KeepSlowerThan(c.minDur))
		}
		if c.usePrefix {
			middlewares = append(middlewares, KeepPathPrefix(c.prefixes...))
		}
		// exercise nil and empty middlewares too
		if rng.Uint64()&1 == 0 {
			middlewares = append(middlewares, nil)
		}
		chained := ChainSampler(NeverSampler(), middlewares...)

		want := chainReference(c)
		if got := chained(in); got != want {
			t.Fatalf("iter %d: chain(%+v) = %v, reference = %v", i, c, got, want)
		}
	}
}

// poolState snapshots everything a straggler write could mutate on a
// sealed event: the WAL, the message, the requested level, and the
// error latch.
type poolState struct {
	fields      []Field
	msg         string
	level       Level
	hasLevel    bool
	hasErr      bool
	sealed      bool
	sealedArmed bool
}

func snapshotState(ev *event) poolState {
	s := ev.state.Load()
	state := walState(s & walStateMask)
	return poolState{
		fields:      append([]Field(nil), ev.fields...),
		msg:         ev.msg,
		level:       ev.requestedLevel,
		hasLevel:    ev.hasRequestedLvl,
		hasErr:      ev.hasErr,
		sealed:      state == walSealed,
		sealedArmed: state == walSealedArmed,
	}
}

func statesEqual(a, b poolState) bool {
	return a.msg == b.msg && a.level == b.level &&
		a.hasLevel == b.hasLevel && a.hasErr == b.hasErr &&
		a.sealed == b.sealed && a.sealedArmed == b.sealedArmed &&
		reflect.DeepEqual(a.fields, b.fields)
}

// capturedEqual compares two captured events (used to pin that the
// records handed to sinks are immutable snapshots).
func capturedEqual(a, b lifeCapture) bool {
	return a.level == b.level && a.message == b.message && bytes.Equal(a.line, b.line)
}

// TestPoolSafetyReplayProperty replays generated program prefixes
// through a stale context after both requests completed.
func TestPoolSafetyReplayProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x90F15E5eed, 0xE6A1E5eed))
	for i := range 400 {
		buf := make([]byte, 2+rng.IntN(160))
		for j := range buf {
			buf[j] = byte(rng.Uint64())
		}
		prog := decodeProgram(buf)
		prog.mode = modeRate1 // pool safety needs live emissions

		// Request 1 runs the program on its own runtime.
		sink1 := &lifeSink{}
		rt1 := MustCompile(Config{Sink: sink1, SamplingRate: 1})
		op1 := Start(context.Background(), rt1, OperationStart{Domain: prog.start, Name: "one"})
		executeProgramOn(prog, op1) // ctx1 := op1.Context() is the stale handle replayed below
		if len(sink1.events) != 1 {
			continue // dropped or never-ended: nothing to protect
		}
		firstCapture := sink1.events[0]

		// Request 2 completes on the same pool.
		sink2 := &lifeSink{}
		rt2 := MustCompile(Config{Sink: sink2, SamplingRate: 1})
		op2 := Start(context.Background(), rt2, OperationStart{Domain: DomainJob, Name: "two"})
		Add(op2.Context(), "second", "owned")
		op2.End(nil)
		if len(sink2.events) != 1 {
			t.Fatalf("iter %d: request 2 emitted %d events", i, len(sink2.events))
		}
		secondCapture := sink2.events[0]
		ev2 := op2.ev
		before2 := snapshotState(ev2)

		// Replay every prefix of the first program through the stale
		// context, including the empty prefix and the full program.
		for k := 0; k <= len(prog.ops); k++ {
			prefix := lifeProgram{mode: prog.mode, start: prog.start, ops: prog.ops[:k]}
			executeProgramOn(prefix, op1) // stale ctx writes: must all no-op
		}

		if !statesEqual(snapshotState(ev2), before2) {
			t.Fatalf("iter %d: replay through the stale context mutated request 2's event", i)
		}
		if !capturedEqual(sink2.events[0], secondCapture) {
			t.Fatalf("iter %d: replay corrupted the captured second event", i)
		}
		if !capturedEqual(sink1.events[0], firstCapture) {
			t.Fatalf("iter %d: replay corrupted the captured first event", i)
		}
	}
}
