package hc

// P6-adjacent size-monotonicity property (dst-research §7.3): adding
// one field with a NEW key to a record never reduces the total encoded
// size. The encoder emits each unique key once in last-occurrence
// order, so a genuinely new key adds its own member (key + value +
// separator) — never removes anything. The property runs across widths
// 1-80 and every value kind; the added key is guaranteed unique so the
// dedupe cannot hide it.

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"
)

// TestRecordEncodeSizeMonotonicProperty generates a record, encodes
// it, appends one field with a brand-new key, and asserts the encoded
// line strictly grows.
func TestRecordEncodeSizeMonotonicProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x51E5E5eed, 0x600D5eed))
	for width := 1; width <= 80; width++ {
		for iter := 0; iter < 50; iter++ {
			used := map[string]bool{}
			fields := make([]Field, 0, width+1)
			for i := 0; i < width; i++ {
				key := fmt.Sprintf("k%03d", rng.IntN(width+5))
				if used[key] {
					continue // keep the generated list unique-keyed
				}
				used[key] = true
				kind := FieldKind(1 + rng.IntN(11)) // every value kind
				fields = append(fields, fieldOf(key, rtFieldValue(rng, kind)))
			}
			// find a brand-new key
			newKey := "k-new"
			for used[newKey] {
				newKey += "x"
			}
			base := recOf(LevelInfo, "m", fields...)
			grown := recOf(LevelInfo, "m", append(append([]Field(nil), fields...), fieldOf(newKey, rtFieldValue(rng, FieldKind(1+rng.IntN(11)))))...)
			before, after := base.Encoded(), grown.Encoded()
			if !bytes.Contains(after, []byte(`"`+newKey+`":`)) {
				t.Fatalf("width %d iter %d: new key %q missing from %s", width, iter, newKey, after)
			}
			if len(after) <= len(before) {
				t.Fatalf("width %d iter %d: adding %q shrank the line (%d -> %d)\nbefore %s\nafter  %s",
					width, iter, newKey, len(before), len(after), before, after)
			}
		}
	}
}
