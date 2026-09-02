package hcjson

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzAppendString checks the equivalence gate continuously: the hybrid
// SWAR path and the vendored zerolog table reference must produce
// byte-identical output for every input. CI runs 60s per target
// (~1M+ execs); the 1M-exec clean gate is a release requirement
// (openspec json-encoder delta).
//
// The body carries a second, semantic oracle (dst-research §6.6): the
// output must always be a valid JSON string, and for valid-UTF-8 input
// it must unescape back to the input exactly. A differential-only
// oracle could not catch a byte class both paths mishandle identically
// — the round-trip property closes that class.
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
		if err := json.Unmarshal(got, &decoded); err != nil {
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
		if err := json.Unmarshal(got, &decoded); err != nil {
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
