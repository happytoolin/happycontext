// Package bridge provides the helpers shared by the first-party sink
// bridges (the slog, zap, and zerolog adapters) and the core encoder, so
// every output path resolves duplicate fields identically and renders
// error fields with the same panic fencing. The bridges live in separate
// modules that require this one, so the shared code is exported here
// rather than under internal/.
package bridge

import (
	"fmt"
	"reflect"
	"slices"
)

// NarrowLimit is the crossover between the allocation-free
// last-occurrence scan and the seen-set path for wide field lists. It is
// a cross-module contract: the canonical encoder and every bridge must
// resolve duplicate keys identically, so they share this one constant
// (pinned by the golden parity tests).
const NarrowLimit = 24

// LastIndices returns the indices of each key's last occurrence, in
// forward order — last-write-wins duplicate resolution. Narrow lists
// use an allocation-free scan; wide ones collect seen keys backward in
// a map. The key function selects each item's comparison key (bridges
// that alias envelope-colliding keys pass the aliased view).
func LastIndices[T any](items []T, key func(T) string) []int {
	if len(items) <= NarrowLimit {
		var stack [NarrowLimit]int // allocation-free narrow path
		n := 0
		for i := range items {
			last := true
			for j := i + 1; j < len(items); j++ {
				if key(items[j]) == key(items[i]) {
					last = false
					break
				}
			}
			if last {
				stack[n] = i
				n++
			}
		}
		return stack[:n:n]
	}
	seen := make(map[string]struct{}, len(items)*2)
	kept := make([]int, 0, len(items))
	for i, item := range slices.Backward(items) {
		if _, dup := seen[key(item)]; dup {
			continue
		}
		seen[key(item)] = struct{}{}
		kept = append(kept, i)
	}
	slices.Reverse(kept)
	return kept
}

// ErrorMessage renders an error field's message, tolerating the two
// ways a user error can explode: typed-nil errors (non-nil interface,
// nil pointer — their Error() nil-derefs, and fmt renders them safely
// as "<nil>") and Error() implementations that panic (the panic is
// contained and the value rendered via fmt, the same fence the core
// encoder applies).
func ErrorMessage(err error) (msg string) {
	if v := reflect.ValueOf(err); v.Kind() == reflect.Pointer && v.IsNil() {
		return fmt.Sprint(err)
	}
	defer func() {
		if recover() != nil {
			msg = fmt.Sprint(err)
		}
	}()
	return err.Error()
}
