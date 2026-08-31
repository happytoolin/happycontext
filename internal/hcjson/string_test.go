package hcjson

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAppendStringTableVendoredBehavior ports the zerolog v1.34.0
// internal/json string encoding tests to the vendored table path.
func TestAppendStringTableVendoredBehavior(t *testing.T) {
	for _, c := range []struct {
		s    string
		want string
	}{
		{`v`, `"v"`},
		{`foo`, `"foo"`},
		{"hello \" world", `"hello \" world"`},
		{"back\\slash", `"back\\slash"`},
		{"tab\t", `"tab\t"`},
		{"\u0000", `"\u0000"`},
		{"\x7f", `"\u007f"`},                 // DEL is escaped
		{"\ufffd", "\"\ufffd\""},             // valid U+FFFD rune passes through raw
		{"foo\ufffdbar", "\"foo\ufffdbar\""}, // pass-through
	} {
		if got := string(appendStringTable(nil, c.s)); got != c.want {
			t.Errorf("appendStringTable(%q) = %s, want %s", c.s, got, c.want)
		}
	}
}

// TestAppendStringUnicode tests that all unicode rune kinds survive the
// table path unchanged (ported from zerolog, extended for the fork).
func TestAppendStringUnicode(t *testing.T) {
	inputs := []string{
		"", "abc", "ö", "\u00f6", "☃", "\u2603", "🍜", "\U0001F35C",
		"\u00e9", "é", "\U0001F600", "ééé",
		strings.Repeat("é", 24), // multi-byte, SWAR length
		strings.Repeat("🍜", 10),
	}
	for _, s := range inputs {
		got := string(Encoder{}.AppendString(nil, s))
		want := string(appendStringTable(nil, s))
		if got != want {
			t.Errorf("AppendString(%q) = %s, want %s (table)", s, got, want)
		}
		if !utf8.ValidString(s) {
			continue
		}
		// valid utf-8 with no escapables must round-trip unescaped
		if !strings.ContainsAny(s, "\"\\\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f") {
			wantPlain := `"` + s + `"`
			if got != wantPlain {
				t.Errorf("AppendString(%q) = %s, want fast-path %s", s, got, wantPlain)
			}
		}
	}
}

// TestAppendStringInvalidUTF8 checks broken sequences become \ufffd.
func TestAppendStringInvalidUTF8(t *testing.T) {
	inputs := []string{
		"\x80",         // continuation byte alone
		"\xc3",         // truncated 2-byte
		"\xe2\x82",     // truncated 3-byte
		"\xf0\x9f\x98", // truncated 4-byte
		"\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8\xf7\xf6\xf5\xf4\xf3\xf2\xf1\xf0\x00",
		strings.Repeat("a\x80b", 8),
	}
	for _, s := range inputs {
		got := string(Encoder{}.AppendString(nil, s))
		want := string(appendStringTable(nil, s))
		if got != want {
			t.Errorf("AppendString(%q) = %s, want %s (table)", s, got, want)
		}
	}
}

// TestAppendStringSWARBoundary pins the length gate: below swarMinLen the
// table path runs, at and above it the SWAR scan engages.
func TestAppendStringSWARBoundary(t *testing.T) {
	for n := 0; n <= 40; n++ {
		s := strings.Repeat("a", n)
		got := string(Encoder{}.AppendString(nil, s))
		want := `"` + s + `"`
		if got != want {
			t.Fatalf("clean ASCII n=%d: got %s want %s", n, got, want)
		}
	}
	// exactly at the gate, with a special byte in the final tail byte
	s := strings.Repeat("a", swarMinLen-1) + "\x00"
	if got := string(Encoder{}.AppendString(nil, s)); got != `"`+strings.Repeat("a", swarMinLen-1)+`\u0000"` {
		t.Fatalf("NUL in tail byte not escaped: %s", got)
	}
}

// TestAppendStringAdversarialPositions plants every byte class the chunk
// scan must reject at every position of a length-24 clean string. This is
// the deterministic net for the broken-zero-detector class of bug (spec
// scenario: a detector missing the &^ x term must fail).
func TestAppendStringAdversarialPositions(t *testing.T) {
	specials := []byte{
		0x00, 0x01, 0x08, 0x09, 0x0a, 0x0d, 0x1b, 0x1f, // controls
		'"', '\\', // must-escape ASCII
		0x7f,                         // DEL — the zerolog table escapes it
		0x80, 0xbf, 0xc3, 0xfe, 0xff, // non-ASCII
	}
	base := strings.Repeat("a", 24)
	for _, sp := range specials {
		for pos := 0; pos < len(base); pos++ {
			s := base[:pos] + string(sp) + base[pos+1:]
			got := string(Encoder{}.AppendString(nil, s))
			want := string(appendStringTable(nil, s))
			if got != want {
				t.Fatalf("special %#x at pos %d: got %s want %s (table)", sp, pos, got, want)
			}
			if sp == '"' && !strings.Contains(got, `\"`) {
				t.Fatalf("quote at pos %d not escaped: %s", pos, got)
			}
		}
	}
	// chunk boundaries explicitly: specials straddling 8-byte edges
	for _, sp := range specials {
		for _, pos := range []int{6, 7, 8, 9, 14, 15, 16, 17, 22, 23} {
			s := strings.Repeat("b", pos) + string(sp) + strings.Repeat("b", 24-pos)
			got := string(Encoder{}.AppendString(nil, s))
			want := string(appendStringTable(nil, s))
			if got != want {
				t.Fatalf("special %#x at boundary pos %d: got %s want %s", sp, pos, got, want)
			}
		}
	}
}

// TestChunkDetector exercises the SWAR chunk predicates directly, at
// every byte position, for the exact input shapes each check must reject.
func TestChunkDetector(t *testing.T) {
	clean := uint64(0x6161616161616161)
	if !chunkIsClean(clean) {
		t.Fatal("clean chunk rejected")
	}
	place := func(i int, b byte) uint64 { return clean&^uint64(0xff)<<(8*i) | uint64(b)<<(8*i) }

	for i := 0; i < 8; i++ {
		if x := clean &^ uint64(0xe0) << (8 * i); chunkIsClean(x) {
			t.Fatalf("control byte at %d accepted", i)
		}
		for _, b := range []byte{0x00, 0x01, 0x1f, '"', 0x5c, 0x7f, 0x80, 0xc3, 0xfe, 0xff} {
			if x := place(i, b); chunkIsClean(x) {
				t.Fatalf("special %#x at byte %d accepted", b, i)
			}
		}
		if x := clean ^ uint64(0x5a)<<(8*i); !chunkIsClean(x) {
			// sanity: ordinary printable byte substitutions stay clean
			t.Fatalf("printable byte at %d rejected", i)
		}
	}

	// canonical zero detector: never misses a zero byte, never invents one
	if hasZero64(clean) {
		t.Fatal("clean chunk reported as containing zero")
	}
	for i := 0; i < 8; i++ {
		if x := clean &^ uint64(0xff) << (8 * i); !hasZero64(x) {
			t.Fatalf("zero at byte %d not detected", i)
		}
	}

	// carry-chain regression: borrow-sensitive chunk shapes (long 0xff/0xfe
	// runs and boundary bytes at 0xa0/0x80) that naive (x + ones)-style
	// checks misread as clean — found by the fuzz corpus
	carryChunks := []uint64{
		0xffffffffffffffff,
		0xfeffffffffffffff, 0xfffeffffffffffff, 0xfffffeffffffffff,
		0xfffefffefffefffe, 0xfefefefefefefefe,
		0xfffefefefefefefe, 0x80ffffffffffffff, 0xffffffffffffff80,
		0xa0a0a0a0a0a0a0a0,
	}
	for _, x := range carryChunks {
		if chunkIsClean(x) {
			t.Fatalf("carry-chain chunk %#016x passed chunkIsClean", x)
		}
	}
}

// TestAppendStringProperty is the encoder's core equivalence gate
// (openspec json-encoder delta, V2_DESIGN §4): 200,000 generated strings —
// random bytes, clean ASCII with injected specials at random positions,
// multi-byte UTF-8, systematic adversarial placements — must encode
// byte-identically through the hybrid SWAR path and the vendored zerolog
// table reference. No benchmark result counts until this passes
// (V2_PLAN.md §05).
func TestAppendStringProperty(t *testing.T) {
	const total = 200_000
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBEEF))

	specials := []byte{
		0x00, 0x01, 0x09, 0x0a, 0x0d, 0x1f, '"', '\\', 0x7f, 0x80, 0xc3, 0xf0, 0xfe, 0xff,
	}
	runes := []rune{'a', 'Z', '0', ' ', 'é', 'ö', '☃', 'ñ', '🍜', '😀', 0x00a0, 0x2028, 0xfffd}

	for i := 0; i < total; i++ {
		var s string
		switch i % 6 {
		case 0: // fully random bytes, every length class
			b := make([]byte, rng.IntN(80))
			for j := range b {
				b[j] = byte(rng.Uint64())
			}
			s = string(b)
		case 1: // clean ASCII of SWAR-relevant length, 0-2 specials injected
			b := make([]byte, 16+rng.IntN(48))
			for j := range b {
				b[j] = byte(0x21 + rng.IntN(0x5d)) // printable-ish ASCII
			}
			for k := 0; k < rng.IntN(3); k++ {
				b[rng.IntN(len(b))] = specials[rng.IntN(len(specials))]
			}
			s = string(b)
		case 2: // valid multi-byte UTF-8
			var sb strings.Builder
			for n := rng.IntN(32) + 1; n > 0; n-- {
				sb.WriteRune(runes[rng.IntN(len(runes))])
			}
			s = sb.String()
		case 3: // quote/backslash-dense
			var sb strings.Builder
			for n := rng.IntN(40) + 16; n > 0; n-- {
				switch rng.IntN(4) {
				case 0:
					sb.WriteByte('"')
				case 1:
					sb.WriteByte('\\')
				default:
					sb.WriteByte(byte('a' + rng.IntN(26)))
				}
			}
			s = sb.String()
		case 4: // systematic: one special at one position of clean fill
			n := rng.IntN(24) + 16
			pos := rng.IntN(n)
			fill := byte('c')
			b := make([]byte, n)
			for j := range b {
				b[j] = fill
			}
			b[pos] = specials[rng.IntN(len(specials))]
			s = string(b)
		case 5: // short strings below the SWAR gate
			b := make([]byte, rng.IntN(16))
			for j := range b {
				b[j] = byte(rng.Uint64())
			}
			s = string(b)
		}

		var base []byte
		if i%7 == 0 {
			base = []byte(`{"k":"prefix`) // non-empty dst, as sinks append into live buffers
		}
		got := Encoder{}.AppendString(append([]byte(nil), base...), s)
		want := appendStringTable(append([]byte(nil), base...), s)
		if !bytes.Equal(got, want) {
			t.Fatalf("property violation at i=%d s=%q:\n got %s\nwant %s (table)", i, s, got, want)
		}
	}
}
