package hcjson

import (
	"strconv"
	"sync/atomic"
	"time"
)

// AppendTime formats the input time with the given layout (as accepted by
// time.Time.AppendFormat) and appends the encoded string to the input byte
// slice.
func (e Encoder) AppendTime(dst []byte, t time.Time, format string) []byte {
	return append(t.AppendFormat(append(dst, '"'), format), '"')
}

// AppendDuration formats the duration as integer units when useInt is
// true (a float otherwise) and appends the encoded value to dst. It mirrors the
// vendored zerolog encoder so duration fields render identically to the
// zerolog adapter (float milliseconds with shortest round-trip precision
// by default).
func (e Encoder) AppendDuration(dst []byte, d time.Duration, unit time.Duration, useInt bool, precision int) []byte {
	if useInt {
		return strconv.AppendInt(dst, int64(d/unit), 10)
	}
	return e.AppendFloat64(dst, float64(d)/float64(unit), precision)
}

// rfc3339Second caches the RFC3339 rendering of one wall-clock second so
// that event timestamps cost an atomic load instead of a full Format call.
type rfc3339Second struct {
	sec int64
	s   string
}

var rfc3339Cache atomic.Pointer[rfc3339Second]

// AppendTimeRFC3339 appends t formatted as RFC3339 (second precision, the
// default zerolog TimeFieldFormat) to dst.
//
// The formatted string is cached per Unix second, so the common case is an
// atomic pointer load. The cache assumes all callers format times in the
// process's local zone (the JSON sink always passes time.Now); formatting
// times from other locations through this function may reuse the wrong
// zone rendering for the shared second. Use AppendTime for arbitrary
// locations. Concurrent use is safe: stores race benignly and the rendered
// value for a given second is deterministic.
func AppendTimeRFC3339(dst []byte, t time.Time) []byte {
	sec := t.Unix()
	if c := rfc3339Cache.Load(); c != nil && c.sec == sec {
		return append(append(append(dst, '"'), c.s...), '"')
	}
	s := t.Format(time.RFC3339)
	rfc3339Cache.Store(&rfc3339Second{sec: sec, s: s})
	return append(append(append(dst, '"'), s...), '"')
}
