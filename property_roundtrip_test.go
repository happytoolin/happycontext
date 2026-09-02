package hc

// P6a property (dst-research §7.1, action plan P6): per-kind field
// round-trip. For every FieldKind, generated values go
// fieldOf(key, v) → Record → Encoded() → parse, and the wire member is
// checked against the kind's contract (see checkFieldWire). The
// generators are PCG-seeded like the hcjson 200k gate; the per-kind
// table in TestEncodedRoundTripAllKinds covers one value per kind,
// this property covers the value space.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
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
			return errors.New(fmt.Sprintf("boom-%d", rng.IntN(1e6)))
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
			for i := 0; i < n; i++ {
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
	for i := 0; i < 2000; i++ {
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
