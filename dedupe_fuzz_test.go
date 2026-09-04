package hc

// FuzzDedupeFields (P2, dst-research §6.2) fuzzes the deduped encode
// across the width boundaries of record.go's dedupe (narrow scan ≤24,
// seen-set wide path) with a width-aware generator: field counts 0-80,
// duplicate density 0-100%, values across the typed kinds. The oracle
// is an independent last-write-wins fold over the generated fields
// (map + last-occurrence ordering), never appendDedupedFields itself.
//
// Canonical-key collisions (user fields named "message"/"time"/"level")
// follow the logrus rename policy (record.go aliasKey): colliding user
// keys are emitted as "fields.message"/"fields.time"/"fields.level" so
// the wire never carries duplicate envelope keys. The envelope keys
// occupy fixed slots — level is member 0, time and message are the
// last two members — and the user span sits strictly between them. The
// oracle folds over the ALIASED keys (a user "message" and a user
// "fields.message" resolve as one last-write-wins member), matching
// the encoder's dedupe.

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

// dedupeKeys is the small key alphabet: k0-k3 for dense duplicates plus
// the canonical-colliding names and a wide unique tail.
var dedupeKeys = []string{"k0", "k1", "k2", "k3", "message", "time", "level", "zz"}

// FuzzDedupeFields generates field lists from three byte streams:
// keys picks the per-field key (alphabet size controls duplicate
// density), vals picks the per-field value kind and operand, widthRaw
// picks the field count.
func FuzzDedupeFields(f *testing.F) {
	// Width boundary seeds: 0, 1, 23/24/25/26 (the crossover), 31/32/33
	// (the seenArr→map handoff), 40, 64, 80, with dense duplicates.
	for _, w := range []int{0, 1, 23, 24, 25, 26, 31, 32, 33, 40, 64, 80} {
		// byte 0 carries the width; byte 1 picks a 4-key alphabet
		// (size = 1 + keyBytes[1]), and keys cycle k0-k3 densely.
		keys := make([]byte, w+1)
		vals := make([]byte, w+1)
		keys[0] = byte(w)
		if w >= 1 {
			keys[1] = 3 // 4-key alphabet for non-empty streams
		}
		for i := 2; i <= w; i++ {
			keys[i] = byte((i - 2) % 4)
			vals[i] = byte(i)
		}
		f.Add(keys, vals)
	}
	// all-same-key and canonical-collision seeds (width in byte 0).
	f.Add([]byte{5, 7, 7, 7, 7, 7}, []byte{0, 1, 2, 3, 4, 5})                     // width 5, all "zz"
	f.Add([]byte{3, 4, 4, 4}, []byte{0, 1, 2, 3})                                 // width 3, all "message"
	f.Add([]byte{3, 5, 5, 5}, []byte{0, 1, 2, 3})                                 // width 3, all "time"
	f.Add([]byte{3, 6, 6, 6}, []byte{0, 1, 2, 3})                                 // width 3, all "level"
	f.Add([]byte{6, 0, 0, 1, 2, 3, 0}, []byte{0, 1, 9, 9, 9, 9, 2})               // width 6, dup at first+last
	f.Add([]byte{}, []byte{})                                                     // empty program
	f.Add([]byte{7, 7, 1, 2, 3, 4, 5, 6}, []byte{10, 10, 10, 10, 10, 10, 10, 10}) // width 7, seven unique keys (8-key alphabet)

	f.Fuzz(func(t *testing.T, keyBytes []byte, valBytes []byte) {
		fields := dedupeFieldsFromBytes(keyBytes, valBytes)
		verifyDedupeFields(t, fields)
	})
}

// dedupeFieldsFromBytes builds the generated field list. The key
// alphabet size is derived from keyBytes so the fuzzer explores every
// duplicate density: size 1 is 100% duplicates, size == width is 0%.
func dedupeFieldsFromBytes(keyBytes, valBytes []byte) []Field {
	width := 0
	if len(keyBytes) > 0 {
		width = int(keyBytes[0]) % 81 // 0-80 fields
	}
	size := 1
	if len(keyBytes) > 1 {
		size = 1 + int(keyBytes[1])%len(dedupeKeys) // 1-8 keys
	}
	fields := make([]Field, 0, width)
	for i := range width {
		var kb byte
		if len(keyBytes) > 1 {
			kb = keyBytes[1+i%(len(keyBytes)-1)]
		}
		key := dedupeKeys[int(kb)%size]
		var vb byte
		if len(valBytes) > 0 {
			vb = valBytes[i%len(valBytes)]
		}
		fields = append(fields, dedupeField(key, vb, i))
	}
	return fields
}

func dedupeField(key string, b byte, i int) Field {
	switch b % 5 {
	case 0:
		return fieldOf(key, int64(int8(b))*int64(i+1))
	case 1:
		return fieldOf(key, fmt.Sprintf("v-%d-%d", i, b))
	case 2:
		return fieldOf(key, float64(int8(b))/7)
	case 3:
		return fieldOf(key, b&1 == 0)
	default:
		return fieldOf(key, time.Duration(int64(int8(b)))*time.Millisecond)
	}
}

// verifyDedupeFields checks one generated field list against the
// reference fold. The user span sits strictly between the envelope
// slots (level at member 0; time and message the last two members), so
// canonical-colliding user keys are compared inside the span and the
// envelope pins stay position-based.
func verifyDedupeFields(t *testing.T, fields []Field) {
	t.Helper()
	r := recOf(LevelInfo, "m", fields...)
	line := r.Encoded()
	if !json.Valid(line) {
		t.Fatalf("encoded line is not valid JSON: %q", line)
	}

	_, members := decodeLineStrict(t, line)
	// Envelope slots: level first, time/message last.
	if len(members) < 3 {
		t.Fatalf("wire has %d members, want >= 3 (level, time, message): %s", len(members), line)
	}
	if members[0].key != "level" {
		t.Fatalf("member 0 = %q, want the canonical level", members[0].key)
	}
	last, secondLast := members[len(members)-1], members[len(members)-2]
	if last.key != "message" || secondLast.key != "time" {
		t.Fatalf("last two members = %q, %q, want time, message", secondLast.key, last.key)
	}
	userSpan := members[1 : len(members)-2]

	// Reference fold over the ALIASED keys: each aliased key's last
	// write, in last-occurrence order (record.go aliases colliding user
	// keys to fields.* before deduping, so the fold keyspace is the
	// aliased one).
	type lw struct {
		field Field
		pos   int
	}
	lastWrite := map[string]lw{}
	for i, f := range fields {
		key := aliasedFieldKey(f.key)
		lastWrite[key] = lw{f, i}
	}
	order := make([]int, 0, len(lastWrite))
	for _, v := range lastWrite {
		order = append(order, v.pos)
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && order[j] < order[j-1]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}

	if len(userSpan) != len(order) {
		t.Fatalf("user span has %d members, want %d unique keys (%s)",
			len(userSpan), len(order), line)
	}
	for i, pos := range order {
		want := lastWrite[aliasedFieldKey(fields[pos].key)].field
		got := userSpan[i]
		if got.key != aliasedFieldKey(want.key) {
			t.Fatalf("user member %d = %q, want %q (last-occurrence order)", i, got.key, aliasedFieldKey(want.key))
		}
		if err := checkFieldWire(want, got.val); err != nil {
			t.Fatalf("user member %d: %v", i, err)
		}
	}
}

// TestDedupeFieldsProperty runs the same oracle over PCG-generated
// field lists — seed-only coverage without the fuzzer. Total: the
// full width × density matrix (81 widths 0-80 × 4 alphabet sizes =
// 324 combos) plus 500 random extra sweeps = 824 checks.
func TestDedupeFieldsProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xDEDE5eed, 0xF1E1D5E))
	check := func(width int, size int) {
		fields := make([]Field, 0, width)
		for i := range width {
			key := dedupeKeys[rng.IntN(size)]
			fields = append(fields, dedupeField(key, byte(rng.Uint64()), i))
		}
		verifyDedupeFields(t, fields)
	}
	for width := range 81 {
		for _, size := range []int{1, 2, 4, 8} {
			check(width, size)
		}
	}
	// random extra sweeps
	for range 500 {
		check(rng.IntN(81), 1+rng.IntN(8))
	}
}
