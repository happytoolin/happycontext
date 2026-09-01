package hcjson

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func TestAppendBytesMirrorsString(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	for i := 0; i < 20_000; i++ {
		b := make([]byte, rng.IntN(80))
		for j := range b {
			b[j] = byte(rng.Uint64())
		}
		got := Encoder{}.AppendBytes(nil, b)
		want := Encoder{}.AppendString(nil, string(b))
		if !bytes.Equal(got, want) {
			t.Fatalf("AppendBytes != AppendString for %q: %s vs %s", b, got, want)
		}
		wantTable := appendBytesTable(nil, b)
		if !bytes.Equal(got, wantTable) {
			t.Fatalf("AppendBytes != table for %q: %s vs %s", b, got, wantTable)
		}
	}
}

func TestAppendBytesBasics(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, `""`},
		{[]byte("v"), `"v"`},
		{[]byte("hello \" world"), `"hello \" world"`},
		{[]byte{0x00}, `"\u0000"`},
		{[]byte{0x7f}, `"\u007f"`},
		{[]byte{0xff}, `"\ufffd"`},
		{bytes.Repeat([]byte("a"), 32), `"` + string(bytes.Repeat([]byte("a"), 32)) + `"`},
	}
	for _, c := range cases {
		if got := string(Encoder{}.AppendBytes(nil, c.in)); got != c.want {
			t.Errorf("AppendBytes(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
