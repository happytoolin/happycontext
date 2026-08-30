package hcjson

import (
	"math"
	"strconv"
	"testing"
)

// TestAppendTypes ports the relevant zerolog v1.34.0 internal/json type
// encoding tests, covering every append the happycontext sink uses.
func TestAppendTypes(t *testing.T) {
	// bools
	if got := string(Encoder{}.AppendBool(nil, true)); got != "true" {
		t.Errorf("AppendBool(true) = %s", got)
	}
	if got := string(Encoder{}.AppendBool(nil, false)); got != "false" {
		t.Errorf("AppendBool(false) = %s", got)
	}

	// ints, each width, extremes
	intCases := []struct {
		got string
		n   int64
	}{
		{string(Encoder{}.AppendInt(nil, 0)), 0},
		{string(Encoder{}.AppendInt(nil, -1)), -1},
		{string(Encoder{}.AppendInt(nil, math.MaxInt)), math.MaxInt},
		{string(Encoder{}.AppendInt(nil, math.MinInt)), math.MinInt},
		{string(Encoder{}.AppendInt8(nil, math.MaxInt8)), math.MaxInt8},
		{string(Encoder{}.AppendInt8(nil, math.MinInt8)), math.MinInt8},
		{string(Encoder{}.AppendInt16(nil, math.MaxInt16)), math.MaxInt16},
		{string(Encoder{}.AppendInt32(nil, math.MaxInt32)), math.MaxInt32},
		{string(Encoder{}.AppendInt64(nil, math.MinInt64)), math.MinInt64},
	}
	for _, c := range intCases {
		if want := itoa(c.n); c.got != want {
			t.Errorf("int append = %s, want %s", c.got, want)
		}
	}

	// uints, each width, extremes
	uintCases := []struct {
		got string
		n   uint64
	}{
		{string(Encoder{}.AppendUint(nil, 0)), 0},
		{string(Encoder{}.AppendUint(nil, 42)), 42},
		{string(Encoder{}.AppendUint8(nil, math.MaxUint8)), math.MaxUint8},
		{string(Encoder{}.AppendUint16(nil, math.MaxUint16)), math.MaxUint16},
		{string(Encoder{}.AppendUint32(nil, math.MaxUint32)), math.MaxUint32},
		{string(Encoder{}.AppendUint64(nil, math.MaxUint64)), math.MaxUint64},
	}
	for _, c := range uintCases {
		if want := utoa(c.n); c.got != want {
			t.Errorf("uint append = %s, want %s", c.got, want)
		}
	}

	// nil and markers
	if got := string(Encoder{}.AppendNil(nil)); got != "null" {
		t.Errorf("AppendNil = %s", got)
	}
	if got := string(Encoder{}.AppendLineBreak(nil)); got != "\n" {
		t.Errorf("AppendLineBreak = %q", got)
	}
}

func itoa(n int64) string  { return strconv.FormatInt(n, 10) }
func utoa(u uint64) string { return strconv.FormatUint(u, 10) }

func TestAppendFloats(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{string(Encoder{}.AppendFloat64(nil, 0, -1)), "0"},
		{string(Encoder{}.AppendFloat64(nil, -1, -1)), "-1"},
		{string(Encoder{}.AppendFloat64(nil, 1e-7, -1)), "1e-7"},
		{string(Encoder{}.AppendFloat64(nil, 1e-9, -1)), "1e-9"},
		{string(Encoder{}.AppendFloat64(nil, 1e21, -1)), "1e+21"},
		{string(Encoder{}.AppendFloat64(nil, 0.000001, -1)), "0.000001"},
		{string(Encoder{}.AppendFloat64(nil, 0.0000001, -1)), "1e-7"},
		{string(Encoder{}.AppendFloat32(nil, 1.5, -1)), "1.5"},
		{string(Encoder{}.AppendFloat64(nil, math.NaN(), -1)), `"NaN"`},
		{string(Encoder{}.AppendFloat64(nil, math.Inf(1), -1)), `"+Inf"`},
		{string(Encoder{}.AppendFloat64(nil, math.Inf(-1), -1)), `"-Inf"`},
		{string(Encoder{}.AppendFloat64(nil, 1.25, 2)), "1.25"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("float append = %s, want %s", c.got, c.want)
		}
	}
}

func TestAppendInterface(t *testing.T) {
	cases := []struct {
		val  any
		want string
	}{
		{nil, "null"},
		{map[string]any{"a": 1, "b": "two"}, `{"a":1,"b":"two"}`},
		{[]any{1, "x"}, `[1,"x"]`},
		{struct {
			A int    `json:"a"`
			B string `json:"b"`
		}{1, "s"}, `{"a":1,"b":"s"}`},
	}
	for _, c := range cases {
		got := string(Encoder{}.AppendInterface(nil, c.val))
		if got != c.want {
			t.Errorf("AppendInterface(%v) = %s, want %s", c.val, got, c.want)
		}
	}
}

func TestAppendKey(t *testing.T) {
	e := Encoder{}
	dst := e.AppendBeginMarker(nil)
	dst = e.AppendKey(dst, "level")
	if string(dst) != `{"level":` {
		t.Fatalf("first key = %s", dst)
	}
	dst = append(dst, '1')
	dst = e.AppendKey(dst, "msg")
	if string(dst) != `{"level":1,"msg":` {
		t.Fatalf("second key = %s", dst)
	}
}
