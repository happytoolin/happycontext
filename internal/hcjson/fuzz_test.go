package hcjson

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzAppendString checks the equivalence gate continuously: the hybrid
// SWAR path and the vendored zerolog table reference must produce
// byte-identical output for every input. CI runs 60s per target
// (~1M+ execs); the 1M-exec clean gate is a release requirement
// (openspec json-encoder delta).
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
	})
}
