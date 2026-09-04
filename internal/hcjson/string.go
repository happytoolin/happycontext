package hcjson

import (
	"encoding/binary"
	"unicode/utf8"
)

const hex = "0123456789abcdef"

// noEscapeTable[b] reports whether byte b needs no escaping inside a JSON
// string. Vendored verbatim from zerolog: printable ASCII except backslash
// and double quote; note 0x7f (DEL) is escaped.
var noEscapeTable = [256]bool{}

func init() {
	for i := 0; i <= 0x7e; i++ {
		noEscapeTable[i] = i >= 0x20 && i != '\\' && i != '"'
	}
}

// swarMinLen is the minimum string length for which the SWAR chunk scan is
// used; below it the table path is faster than setting up the scan.
const swarMinLen = 16

// AppendString encodes the input string to json and appends the encoded
// string to the input byte slice. Strings of swarMinLen bytes or more
// are first scanned with the SWAR fast path; clean printable-ASCII
// strings are appended in bulk, everything else goes through
// appendStringTable, the reference (zerolog) encoding.
func (e Encoder) AppendString(dst []byte, s string) []byte {
	if len(s) >= swarMinLen && isCleanASCII(s) {
		return append(append(append(dst, '"'), s...), '"')
	}
	return appendStringTable(dst, s)
}

// appendStringTable is the vendored zerolog v1.34.0 AppendString: a
// byte-by-byte scan over noEscapeTable with a complex path for anything
// that needs escaping. It is the correctness oracle for the SWAR fast
// path (property and fuzz tests).
func appendStringTable(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		if !noEscapeTable[s[i]] {
			dst = appendStringComplex(dst, s, i)
			return append(dst, '"')
		}
	}
	// Clean string: appended in bulk.
	dst = append(dst, s...)
	return append(dst, '"')
}

// appendStringComplex is used by appendStringTable to take over an in
// progress JSON string encoding that encountered a character that needs
// to be encoded.
func appendStringComplex(dst []byte, s string, i int) []byte {
	start := 0
	for i < len(s) {
		b := s[i]
		if b >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				// Invalid sequence: append the pending simple prefix and
				// the replacement character.
				if start < i {
					dst = append(dst, s[start:i]...)
				}
				dst = append(dst, `\ufffd`...)
				i += size
				start = i
				continue
			}
			i += size
			continue
		}
		if noEscapeTable[b] {
			i++
			continue
		}
		// Needs encoding: append the pending simple prefix, then the
		// escaped byte.
		if start < i {
			dst = append(dst, s[start:i]...)
		}
		switch b {
		case '"', '\\':
			dst = append(dst, '\\', b)
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xF])
		}
		i++
		start = i
	}
	if start < len(s) {
		dst = append(dst, s[start:]...)
	}
	return dst
}

// isCleanASCII reports whether every byte of s is printable ASCII that
// needs no JSON escaping (0x20..0x7e except '"' and '\\'), using a SWAR
// scan of 8 bytes per iteration. It is conservative: any suspicious
// chunk (including chunks that only look suspicious because of SWAR
// borrow artifacts) returns false, so a false positive costs
// performance, never correctness.
func isCleanASCII(s string) bool {
	for len(s) >= 8 {
		if !chunkIsClean(binary.LittleEndian.Uint64([]byte(s[:8]))) {
			return false
		}
		s = s[8:]
	}
	for i := 0; i < len(s); i++ {
		if !noEscapeTable[s[i]] {
			return false
		}
	}
	return true
}

// chunkIsClean reports whether all 8 bytes of the chunk are printable
// ASCII (0x20..0x7e) other than '"' and '\\'. The range predicate
//
//	((x - 0x2020..) | (0x7e7e.. - x)) & 0x8080..
//
// flags every byte outside [0x20,0x7e] in one pass: control bytes
// underflow the first subtraction, DEL and non-ASCII the second. A
// borrow can strip a high bit only when the no-borrow result is exactly
// 0x80, and no byte value makes both terms land there at once — so at
// least one term keeps its high bit for every out-of-range byte.
// Borrow-induced hits on in-range bytes are false positives, which only
// send the string to the table path.
func chunkIsClean(x uint64) bool {
	if ((x-0x2020202020202020)|(0x7e7e7e7e7e7e7e7e-x))&swarHighs != 0 {
		return false // control, DEL, or non-ASCII
	}
	// Bytes equal to '"' or '\\', via exact per-value zero detection.
	// (AND-ing the two XOR masks is NOT sound: ordinary bytes whose quote-
	// and slash-XORs have disjoint bits AND to zero and masquerade as hits.)
	return !hasValue64(x, swarQuote) && !hasValue64(x, swarSlash)
}

const (
	swarOnes  = 0x0101010101010101
	swarHighs = 0x8080808080808080
	swarQuote = 0x2222222222222222 // '"'
	swarSlash = 0x5c5c5c5c5c5c5c5c // '\\'
)

// hasZero64 reports whether the 8-byte word x contains a zero byte.
//
// This MUST stay in the canonical SWAR form
//
//	(x - 0x0101010101010101) &^ x & 0x8080808080808080
//
// Dropping the &^ x term compiles and runs several times faster —
// because it never detects anything: a zero byte clears its own high
// bit in x, so the masked result is always zero and escaping is
// silently skipped. V2_PLAN.md §05 documents the incident; the property
// test in this package fails any such regression.
func hasZero64(x uint64) bool {
	return (x-swarOnes)&^x&swarHighs != 0
}

// hasValue64 reports whether any byte of x equals the broadcast byte c
// (e.g. swarQuote), via the canonical zero detector on the XOR mask.
// Exact up to benign borrow-chain false positives on the byte above a
// match; it never misses a match.
func hasValue64(x, c uint64) bool {
	return hasZero64(x ^ c)
}
