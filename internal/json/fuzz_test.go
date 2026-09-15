package json

import (
	"bytes"
	stdjson "encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzAppendString checks the equivalence gate continuously: the hybrid
// SWAR path and the vendored zerolog table reference must produce
// byte-identical output for every input. CI runs 60s per target
// (~1M+ execs); the 1M-exec clean gate is a release requirement.
//
// The body carries a second, semantic oracle (the zap adoption —
// FuzzSafeAppendStringLike + roundTripsCorrectly*, zapcore/
// json_encoder_impl_test.go; see also dst-research §6.6): the output
// must always be a valid JSON string, and for valid-UTF-8 input it
// must unescape back to the input exactly. A differential-only oracle
// could not catch a byte class both paths mishandle identically — the
// round-trip property closes that class.
func FuzzAppendString(f *testing.F) {
	seeds := []string{
		"",
		"simple",
		`quo"te`,
		`back\slash`,
		"\x00\x01\x1f\x7f\x80\xff",
		"tab\tnewline\n\r",
		strings.Repeat("a", 15),
		strings.Repeat("a", 16),
		strings.Repeat("a", 23) + "\x00",
		strings.Repeat("a", 31) + `\"` + strings.Repeat("b", 8),
		strings.Repeat("quote\"back\\", 4),
		"unicode é☃🍜 \U0001F600",
		"\xc3\xa9 broken \x80 tail",
		strings.Repeat("\xff\xfe", 16),
		"/api/v1/users/12345/orders?include=items&fields=all",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := Encoder{}.AppendString(nil, s)
		want := appendStringTable(nil, s)
		if !bytes.Equal(got, want) {
			t.Fatalf("SWAR %q != table %q (got %s want %s)", s, s, got, want)
		}
		var decoded string
		if err := stdjson.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("output is not a valid JSON string: %v (input %q)", err, s)
		}
		if utf8.ValidString(s) && decoded != s {
			t.Fatalf("round-trip: input %q decoded to %q (wire %s)", s, decoded, got)
		}
		// invalid UTF-8 must normalize to replacement runes with the
		// documented per-byte multiplicity (each byte of a broken sequence
		// maps to one U+FFFD; a genuine U+FFFD rune passes through raw), so
		// the decoded count must equal invalid bytes + genuine U+FFFD runes.
		if !utf8.ValidString(s) {
			want := 0
			for i := 0; i < len(s); {
				r, size := utf8.DecodeRuneInString(s[i:])
				if r == utf8.RuneError && size == 1 {
					want++ // invalid byte → one replacement
					i++
					continue
				}
				if r == 0xfffd {
					want++ // genuine replacement rune passes through
				}
				i += size
			}
			if got := strings.Count(decoded, "\ufffd"); got != want {
				t.Fatalf("round-trip: invalid input %q decoded to %q (want %d replacements, got %d)", s, decoded, want, got)
			}
		}
	})
}

// FuzzAppendBytes is the byte-slice mirror of FuzzAppendString.
func FuzzAppendBytes(f *testing.F) {
	seeds := [][]byte{
		nil,
		[]byte("simple"),
		{0x00, 0x01, 0x1f, 0x22, 0x5c, 0x7f, 0x80, 0xff},
		bytes.Repeat([]byte("a"), 16),
		bytes.Repeat([]byte("a"), 15),
		bytes.Repeat([]byte{0x7f}, 24),
	}
	for _, b := range seeds {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		got := Encoder{}.AppendBytes(nil, b)
		want := appendBytesTable(nil, b)
		if !bytes.Equal(got, want) {
			t.Fatalf("SWAR %q != table %q (got %s want %s)", b, b, got, want)
		}
		var decoded string
		if err := stdjson.Unmarshal(got, &decoded); err != nil {
			t.Fatalf("output is not a valid JSON string: %v (input %q)", err, b)
		}
		if utf8.Valid(b) && decoded != string(b) {
			t.Fatalf("round-trip: input %q decoded to %q (wire %s)", b, decoded, got)
		}
		// invalid UTF-8 must normalize with the per-byte multiplicity
		// documented above (each broken byte → one U+FFFD; genuine U+FFFD
		// bytes pass through) — the shared-blind-spot half of the semantic
		// check, in the count form that matches the vendored mapping.
		if !utf8.Valid(b) {
			want := 0
			for i := 0; i < len(b); {
				r, size := utf8.DecodeRune(b[i:])
				if r == utf8.RuneError && size == 1 {
					want++
					i++
					continue
				}
				if r == 0xfffd {
					want++
				}
				i += size
			}
			if got := strings.Count(decoded, "\ufffd"); got != want {
				t.Fatalf("round-trip: invalid input %q decoded to %q (want %d replacements, got %d)", b, decoded, want, got)
			}
		}
	})
}

// FuzzAppendInterface closes the remaining encoder gap (dst-research
// §6.4, P4): the any-fallback marshaller has zero fuzz coverage while
// the string/float/time paths have differential or round-trip oracles.
// The oracle here is parsed equivalence with encoding/json plus
// no-panic/valid-output invariants — HTML escaping is excluded from
// equivalence on purpose (the fork disables it; pinned separately by
// TestAppendInterfaceNoHTMLEscape).
func FuzzAppendInterface(f *testing.F) {
	seeds := []string{
		`null`, `1`, `-2.5`, `true`, `"str"`, `"héllo ☃"`,
		`[1,"x",false,null]`,
		`{"a":1,"b":"two","c":[true,null]}`,
		`{"nested":{"deep":{"deeper":[1,2,3]}}}`,
		`{"html":"<b>a&b</b>"}`,
		`[]`, `{}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		var v any
		if err := stdjson.Unmarshal(raw, &v); err != nil {
			return // not a valid JSON shape; nothing to check
		}
		got := Encoder{}.AppendInterface(nil, v)
		if len(got) == 0 {
			t.Fatal("empty output")
		}
		if !stdjson.Valid(got) {
			t.Fatalf("AppendInterface(%v) produced invalid JSON: %s", v, got)
		}
		want, err := stdjson.Marshal(v)
		if err != nil {
			t.Fatalf("oracle marshal failed: %v", err)
		}
		var gotV, wantV any
		if err := stdjson.Unmarshal(got, &gotV); err != nil {
			t.Fatalf("output unparseable: %v", err)
		}
		if err := stdjson.Unmarshal(want, &wantV); err != nil {
			t.Fatalf("oracle unparseable: %v", err)
		}
		if !jsonDecodedEqual(gotV, wantV) {
			t.Fatalf("AppendInterface(%v) = %s, parses to %v; stdjson.Marshal parses to %v", v, got, gotV, wantV)
		}
	})
}

// jsonDecodedEqual compares two stdjson.Unmarshal results (plain float64
// numbers — the parser erases int/float distinctions) and compares
// float64s bitwise. The unolog package mirrors this in
// property_test.go's jsonSemanticEqual, which additionally
// accepts stdjson.Number from UseNumber decoders; the helpers cannot be
// shared because test-only code is package-private.
func jsonDecodedEqual(a, b any) bool {
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonDecodedEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bvv, ok := bv[k]
			if !ok || !jsonDecodedEqual(v, bvv) {
				return false
			}
		}
		return true
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case nil:
		return b == nil
	default:
		return false
	}
}

// panickingMarshaler pins the panicking-MarshalJSON decision
// (dst-research P4, slog's !PANIC precedent): encoding/json lets a user
// MarshalJSON panic propagate (both the classic implementation and the
// go1.27 json/v2 default), so the fork's jsonMarshal recovers it into
// the documented fallback string — the sink must not unwind End because
// of a broken user marshaler, and the wire behavior must be identical
// across the Go 1.25–1.27 matrix.
type panickingMarshaler struct{}

func (panickingMarshaler) MarshalJSON() ([]byte, error) { panic("marshaler boom") }

type nilReceiverMarshaler struct{}

func (*nilReceiverMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`"ok"`), nil
}

func TestAppendInterfaceMarshalerEdgeCases(t *testing.T) {
	// a panicking MarshalJSON must not crash the sink: the fork recovers
	// it into the documented fallback string (slog renders !PANIC; the
	// vendored zerolog shape is the marshaling-error string)
	got := string(Encoder{}.AppendInterface(nil, panickingMarshaler{}))
	if !strings.HasPrefix(got, `"marshaling error:`) {
		t.Fatalf("panicking marshaler rendered %s, want marshaling-error string", got)
	}
	var back string
	if err := stdjson.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("fallback not a valid JSON string: %v (%s)", err, got)
	}

	// a nil-receiver MarshalJSON (valid but unusual) must marshal like
	// stdlib: the method is callable on a nil *T, stdlib calls it, so
	// the fallback must match stdjson.Marshal's bytes
	val := (*nilReceiverMarshaler)(nil)
	got = string(Encoder{}.AppendInterface(nil, val))
	want, err := stdjson.Marshal(val)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("nil-receiver marshaler: got %s want %s", got, want)
	}

	// channels and funcs are unmarshalable: error string, not a hang
	got = string(Encoder{}.AppendInterface(nil, make(chan int)))
	if !strings.HasPrefix(got, `"marshaling error:`) {
		t.Fatalf("channel rendered %s, want marshaling-error string", got)
	}
}
