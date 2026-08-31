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
	dst = append(dst, s...)
	return append(dst, '"')
}

// appendBytesComplex is a mirror of the appendStringComplex
// with []byte arg. Vendored verbatim from zerolog.
func appendBytesComplex(dst, s []byte, i int) []byte {
	start := 0
	for i < len(s) {
		b := s[i]
		if b >= utf8.RuneSelf {
			r, size := utf8.DecodeRune(s[i:])
			if r == utf8.RuneError && size == 1 {
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
		// We encountered a character that needs to be encoded.
		// Let's append the previous simple characters to the byte slice
		// and switch our operation to read and encode the remainder
		// characters byte-by-byte.
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
