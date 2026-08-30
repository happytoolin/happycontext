package hcjson

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// jsonMarshal is the fallback marshaller used by AppendInterface for
// values without a dedicated append method. It is a variable to keep the
// vendored zerolog seam (JSONMarshalFunc) swappable in tests.
var jsonMarshal = json.Marshal

// AppendNil inserts a 'Nil' object into the dst byte array.
func (Encoder) AppendNil(dst []byte) []byte {
	return append(dst, "null"...)
}

// AppendBeginMarker inserts a map start into the dst byte array.
func (Encoder) AppendBeginMarker(dst []byte) []byte {
	return append(dst, '{')
}

// AppendEndMarker inserts a map end into the dst byte array.
func (Encoder) AppendEndMarker(dst []byte) []byte {
	return append(dst, '}')
}

// AppendLineBreak appends a line break.
func (Encoder) AppendLineBreak(dst []byte) []byte {
	return append(dst, '\n')
}

// AppendBool converts the input bool to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendBool(dst []byte, val bool) []byte {
	return strconv.AppendBool(dst, val)
}

// AppendInt converts the input int to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendInt(dst []byte, val int) []byte {
	return strconv.AppendInt(dst, int64(val), 10)
}

// AppendInt8 converts the input int8 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendInt8(dst []byte, val int8) []byte {
	return strconv.AppendInt(dst, int64(val), 10)
}

// AppendInt16 converts the input int16 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendInt16(dst []byte, val int16) []byte {
	return strconv.AppendInt(dst, int64(val), 10)
}

// AppendInt32 converts the input int32 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendInt32(dst []byte, val int32) []byte {
	return strconv.AppendInt(dst, int64(val), 10)
}

// AppendInt64 converts the input int64 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendInt64(dst []byte, val int64) []byte {
	return strconv.AppendInt(dst, val, 10)
}

// AppendUint converts the input uint to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendUint(dst []byte, val uint) []byte {
	return strconv.AppendUint(dst, uint64(val), 10)
}

// AppendUint8 converts the input uint8 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendUint8(dst []byte, val uint8) []byte {
	return strconv.AppendUint(dst, uint64(val), 10)
}

// AppendUint16 converts the input uint16 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendUint16(dst []byte, val uint16) []byte {
	return strconv.AppendUint(dst, uint64(val), 10)
}

// AppendUint32 converts the input uint32 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendUint32(dst []byte, val uint32) []byte {
	return strconv.AppendUint(dst, uint64(val), 10)
}

// AppendUint64 converts the input uint64 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendUint64(dst []byte, val uint64) []byte {
	return strconv.AppendUint(dst, val, 10)
}

func appendFloat(dst []byte, val float64, bitSize, precision int) []byte {
	// JSON does not permit NaN or Infinity. A typical JSON encoder would fail
	// with an error, but a logging library wants the data to get through so we
	// make a tradeoff and store those types as string.
	switch {
	case math.IsNaN(val):
		return append(dst, `"NaN"`...)
	case math.IsInf(val, 1):
		return append(dst, `"+Inf"`...)
	case math.IsInf(val, -1):
		return append(dst, `"-Inf"`...)
	}
	// convert as if by es6 number to string conversion
	// see also https://cs.opensource.google/go/go/+/refs/tags/go1.20.3:src/encoding/json/encode.go;l=573
	strFmt := byte('f')
	// If precision is set to a value other than -1, we always just format the float using that precision.
	if precision == -1 {
		// Use float32 comparisons for underlying float32 value to get precise cutoffs right.
		if abs := math.Abs(val); abs != 0 {
			if bitSize == 64 && (abs < 1e-6 || abs >= 1e21) || bitSize == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
				strFmt = 'e'
			}
		}
	}
	dst = strconv.AppendFloat(dst, val, strFmt, precision, bitSize)
	if strFmt == 'e' {
		// Clean up e-09 to e-9
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

// AppendFloat32 converts the input float32 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendFloat32(dst []byte, val float32, precision int) []byte {
	return appendFloat(dst, float64(val), 32, precision)
}

// AppendFloat64 converts the input float64 to a string and
// appends the encoded string to the input byte slice.
func (Encoder) AppendFloat64(dst []byte, val float64, precision int) []byte {
	return appendFloat(dst, val, 64, precision)
}

// AppendInterface marshals the input interface to a string and
// appends the encoded string to the input byte slice.
func (e Encoder) AppendInterface(dst []byte, i any) []byte {
	marshaled, err := jsonMarshal(i)
	if err != nil {
		return e.AppendString(dst, fmt.Sprintf("marshaling error: %v", err))
	}
	return append(dst, marshaled...)
}
