// Package hcjson is a minimal append-only JSON encoder, vendored from
// zerolog v1.34.0's internal/json package (MIT; see LICENSE and README.md
// in this directory). The hot paths happycontext uses (string/bytes
// escaping, scalars, time, interface fallback) were kept verbatim in
// shape — including the unused-width scalar constructors — for easy
// diffing against the upstream original.
//
// The upgrade over the vendored original is a hybrid SWAR escape fast path
// in AppendString/AppendBytes: strings of 16 bytes or more are scanned 8
// bytes at a time; if every byte is clean printable ASCII the string is
// appended without any per-byte table lookups. Shorter or suspicious
// strings fall back to the original byte-by-byte table path, so the output
// is byte-identical to the zerolog reference for every input (verified by
// property and fuzz tests in this package).
//
// All operations append to dst and return the extended slice; nothing here
// allocates on its own beyond what strconv/json.Marshal require.
package hcjson

// Encoder is a stateless value type; methods are safe for concurrent use.
type Encoder struct{}

// AppendKey appends a new key to the output JSON. It requires a non-empty
// dst (typically starting from AppendBeginMarker), matching the vendored
// zerolog contract.
func (e Encoder) AppendKey(dst []byte, key string) []byte {
	if dst[len(dst)-1] != '{' {
		dst = append(dst, ',')
	}
	return append(e.AppendString(dst, key), ':')
}
