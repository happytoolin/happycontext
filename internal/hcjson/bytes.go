package hcjson

import (
	"encoding/binary"
	"unicode/utf8"
)

// AppendBytes is a mirror of AppendString with []byte arg: long clean
// chunks go through the SWAR fast path, everything else through the table
// path.
func (e Encoder) AppendBytes(dst, s []byte) []byte {
	if len(s) >= swarMinLen && isCleanASCIIBytes(s) {
		return append(append(append(dst, '"'), s...), '"')
	}
	return appendBytesTable(dst, s)
}

// appendBytesTable is the vendored zerolog v1.34.0 AppendBytes (table
// path); the correctness oracle for the byte-slice SWAR fast path.
func appendBytesTable(dst, s []byte) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		if !noEscapeTable[s[i]] {
			dst = appendBytesComplex(dst, s, i)
			return append(dst, '"')
		}
	}
	// Clean string: appended in bulk.
	dst = append(dst, s...)
	return append(dst, '"')
}

// appendBytesComplex is a mirror of appendStringComplex with []byte arg.
func appendBytesComplex(dst, s []byte, i int) []byte {
	start := 0
	for i < len(s) {
		b := s[i]
		if b >= utf8.RuneSelf {
			r, size := utf8.DecodeRune(s[i:])
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

// isCleanASCIIBytes is isCleanASCII for byte slices.
func isCleanASCIIBytes(s []byte) bool {
	for len(s) >= 8 {
		if !chunkIsClean(binary.LittleEndian.Uint64(s[:8])) {
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
