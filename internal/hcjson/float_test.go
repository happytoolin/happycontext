package hcjson

import (
	"encoding/json"
	"math"
	"testing"
)

// float64Tests and float32Tests are ported from zerolog v1.35.0's
// internal/json/float_test.go (MIT — same lineage as the vendored
// encoder), extended with the −0 and float32-precision cases the DST
// research flagged (V2_PLAN §05 / dst-research §6.4).
var float64Tests = []struct {
	name string
	val  float64
	want string
}{
	{"Positive integer", 1234.0, "1234"},
	{"Negative integer", -5678.0, "-5678"},
	{"Positive decimal", 12.3456, "12.3456"},
	{"Negative decimal", -78.9012, "-78.9012"},
	{"Large positive number", 123456789.0, "123456789"},
	{"Large negative number", -987654321.0, "-987654321"},
	{"Zero", 0.0, "0"},
	{"Negative zero", math.Copysign(0, -1), "-0"},
	{"Smallest positive value", math.SmallestNonzeroFloat64, "5e-324"},
	{"Largest positive value", math.MaxFloat64, "1.7976931348623157e+308"},
	{"Smallest negative value", -math.SmallestNonzeroFloat64, "-5e-324"},
	{"Largest negative value", -math.MaxFloat64, "-1.7976931348623157e+308"},
	{"NaN", math.NaN(), `"NaN"`},
	{"+Inf", math.Inf(1), `"+Inf"`},
	{"-Inf", math.Inf(-1), `"-Inf"`},
	{"Clean up e-09 to e-9 case 1", 1e-9, "1e-9"},
	{"Clean up e-09 to e-9 case 2", -2.236734e-9, "-2.236734e-9"},
	{"2^53 boundary", 1 << 53, "9007199254740992"},
	{"2^53 plus one ulp", float64(1<<53) + 2, "9007199254740994"},
}

var float32Tests = []struct {
	name string
	val  float32
	want string
}{
	{"Positive integer", 1234.0, "1234"},
	{"Negative integer", -5678.0, "-5678"},
	{"Positive decimal", 12.3456, "12.3456"},
	{"Negative decimal", -78.9012, "-78.9012"},
	{"Large positive number", 123456789.0, "123456790"},
	{"Large negative number", -987654321.0, "-987654340"},
	{"Zero", 0.0, "0"},
	{"Negative zero", float32(math.Copysign(0, -1)), "-0"},
	{"Smallest positive value", math.SmallestNonzeroFloat32, "1e-45"},
	{"Largest positive value", math.MaxFloat32, "3.4028235e+38"},
	{"Smallest negative value", -math.SmallestNonzeroFloat32, "-1e-45"},
	{"Largest negative value", -math.MaxFloat32, "-3.4028235e+38"},
	{"NaN", float32(math.NaN()), `"NaN"`},
	{"+Inf", float32(math.Inf(1)), `"+Inf"`},
	{"-Inf", float32(math.Inf(-1)), `"-Inf"`},
	{"Clean up e-09 to e-9 case 1", 1e-9, "1e-9"},
	{"Clean up e-09 to e-9 case 2", -2.236734e-9, "-2.236734e-9"},
	// the float32-preserving wire contract: 0.1 must not widen to
	// 0.10000000149011612 (v0 adapter parity; slog bridge documents the
	// host limitation)
	{"Float32 precision preserved", 0.1, "0.1"},
}

func TestAppendFloat64Table(t *testing.T) {
	for _, tc := range float64Tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string((Encoder{}).AppendFloat64(nil, tc.val, -1)); got != tc.want {
				t.Errorf("AppendFloat64(%v) = %s, want %s", tc.val, got, tc.want)
			}
		})
	}
}

func TestAppendFloat32Table(t *testing.T) {
	for _, tc := range float32Tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string((Encoder{}).AppendFloat32(nil, tc.val, -1)); got != tc.want {
				t.Errorf("AppendFloat32(%v) = %s, want %s", tc.val, got, tc.want)
			}
		})
	}
}

// FuzzAppendFloat64 checks the vendored float policy continuously: for
// NaN/±Inf the three quoted-string shapes, for finite values exact
// parity with encoding/json and an exact json.Unmarshal round-trip
// (the DST research's shared-blind-spot closer for the numeric path —
// a differential-only oracle could not catch a policy both paths got
// wrong).
func FuzzAppendFloat64(f *testing.F) {
	for _, tc := range float64Tests {
		f.Add(tc.val)
	}
	f.Add(0.1)
	f.Add(-0.1)
	f.Add(math.Float64frombits(0x7ff0000000000001)) // signaling NaN payload
	f.Add(math.Float64frombits(0x7ff8000000000000)) // quiet NaN
	f.Fuzz(func(t *testing.T, val float64) {
		actual := (Encoder{}).AppendFloat64(nil, val, -1)
		if len(actual) == 0 {
			t.Fatal("empty buffer")
		}
		if actual[0] == '"' {
			switch string(actual) {
			case `"NaN"`:
				if !math.IsNaN(val) {
					t.Fatalf("expected %v got NaN", val)
				}
			case `"+Inf"`:
				if !math.IsInf(val, 1) {
					t.Fatalf("expected %v got +Inf", val)
				}
			case `"-Inf"`:
				if !math.IsInf(val, -1) {
					t.Fatalf("expected %v got -Inf", val)
				}
			default:
				t.Fatalf("unexpected string rendering: %s", actual)
			}
			return
		}
		if expected, err := json.Marshal(val); err != nil {
			t.Error(err)
		} else if string(actual) != string(expected) {
			t.Errorf("json.Marshal parity: expected %s, got %s", expected, actual)
		}
		var parsed float64
		if err := json.Unmarshal(actual, &parsed); err != nil {
			t.Fatal(err)
		}
		if parsed != val && !(parsed != parsed && val != val) { // NaN already handled above
			t.Fatalf("round-trip: expected %v, got %v (wire %s)", val, parsed, actual)
		}
		// −0 must survive the round trip bitwise (sign preserved)
		if val == 0 && math.Signbit(val) && !math.Signbit(parsed) {
			t.Fatalf("negative zero lost: wire %s", actual)
		}
	})
}

// FuzzAppendFloat32 is the float32 mirror; the round-trip comparison is
// done in float64 after re-widening, matching the wire contract (the
// bytes are parsed as a JSON number, then compared against the original
// float32 value widened — exact equality, no tolerance).
func FuzzAppendFloat32(f *testing.F) {
	for _, tc := range float32Tests {
		f.Add(tc.val)
	}
	f.Add(float32(0.1))
	f.Add(float32(1) / 3)
	f.Fuzz(func(t *testing.T, val float32) {
		actual := (Encoder{}).AppendFloat32(nil, val, -1)
		if len(actual) == 0 {
			t.Fatal("empty buffer")
		}
		if actual[0] == '"' {
			switch string(actual) {
			case `"NaN"`:
				if !math.IsNaN(float64(val)) {
					t.Fatalf("expected %v got NaN", val)
				}
			case `"+Inf"`:
				if !math.IsInf(float64(val), 1) {
					t.Fatalf("expected %v got +Inf", val)
				}
			case `"-Inf"`:
				if !math.IsInf(float64(val), -1) {
					t.Fatalf("expected %v got -Inf", val)
				}
			default:
				t.Fatalf("unexpected string rendering: %s", actual)
			}
			return
		}
		if expected, err := json.Marshal(val); err != nil {
			t.Error(err)
		} else if string(actual) != string(expected) {
			t.Errorf("json.Marshal parity: expected %s, got %s", expected, actual)
		}
		var parsed32 float32
		if err := json.Unmarshal(actual, &parsed32); err != nil {
			t.Fatal(err)
		}
		// the wire contract is 32-bit: parse back at float32 width and
		// require exact equality (parsing the shortest-32 decimal as
		// float64 would land on a different float64 than the widened
		// value — that gap is expected and not a bug)
		if parsed32 != val && !(parsed32 != parsed32 && val != val) {
			t.Fatalf("round-trip: expected %v, got %v (wire %s)", val, parsed32, actual)
		}
	})
}
