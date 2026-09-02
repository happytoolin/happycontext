package hcjson

import (
	"encoding/json"
	"strings"
	"testing"
)

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
		if err := json.Unmarshal(raw, &v); err != nil {
			return // not a valid JSON shape; nothing to check
		}
		got := Encoder{}.AppendInterface(nil, v)
		if len(got) == 0 {
			t.Fatal("empty output")
		}
		if !json.Valid(got) {
			t.Fatalf("AppendInterface(%v) produced invalid JSON: %s", v, got)
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("oracle marshal failed: %v", err)
		}
		var gotV, wantV any
		if err := json.Unmarshal(got, &gotV); err != nil {
			t.Fatalf("output unparseable: %v", err)
		}
		if err := json.Unmarshal(want, &wantV); err != nil {
			t.Fatalf("oracle unparseable: %v", err)
		}
		if !jsonDecodedEqual(gotV, wantV) {
			t.Fatalf("AppendInterface(%v) = %s, parses to %v; json.Marshal parses to %v", v, got, gotV, wantV)
		}
	})
}

// jsonDecodedEqual compares two json.Unmarshal results (plain float64
// numbers — the parser erases int/float distinctions) and compares
// float64s bitwise. The hc package mirrors this in
// lifecycle_fuzz_test.go's jsonSemanticEqual, which additionally
// accepts json.Number from UseNumber decoders; the helpers cannot be
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
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("fallback not a valid JSON string: %v (%s)", err, got)
	}

	// a nil-receiver MarshalJSON (valid but unusual) must marshal like
	// stdlib: the method is callable on a nil *T, stdlib calls it, so
	// the fallback must match json.Marshal's bytes
	val := (*nilReceiverMarshaler)(nil)
	got = string(Encoder{}.AppendInterface(nil, val))
	want, err := json.Marshal(val)
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
