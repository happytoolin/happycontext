//go:build go1.27

package benches_test

import (
	"encoding/json"
	"encoding/json/jsontext"
	"io"
	"strconv"
	"testing"
	"time"

	hc "github.com/happytoolin/happycontext"
)

// jsontextSink is the maintenance-free comparator backend: the same event
// shape as hc.JSONSink, encoded with Go 1.27's encoding/json/jsontext
// appends instead of the forked zerolog encoder. It exists so the fork can
// be re-raced against the stdlib backend per Go release
// (V2_DESIGN §8 encoder decision; amendment 10 keeps this comparator in
// benches/, out of the production module).
type jsontextSink struct {
	w io.Writer
}

func (s *jsontextSink) Write(level hc.Level, message string, fields map[string]any) {
	dst := make([]byte, 0, 512)
	dst = append(dst, `{"level":"`...)
	dst = append(dst, jsonLevelText(level)...)
	dst = append(dst, `","time":"`...)
	dst = time.Now().AppendFormat(dst, time.RFC3339)
	dst = append(dst, '"')
	for k, v := range fields {
		dst = append(dst, ',', '"')
		dst, _ = jsontext.AppendQuote(dst, k)
		dst = append(dst, '"', ':')
		dst = appendJSONTextValue(dst, v)
	}
	if message == "" {
		message = hc.DefaultMessage
	}
	dst = append(dst, ',', '"')
	dst, _ = jsontext.AppendQuote(dst, "message")
	dst = append(dst, '"', ':')
	dst, _ = jsontext.AppendQuote(dst, message)
	dst = append(dst, '}', '\n')
	_, _ = s.w.Write(dst)
}

func jsonLevelText(level hc.Level) string {
	switch level {
	case hc.LevelDebug:
		return "debug"
	case hc.LevelWarn:
		return "warn"
	case hc.LevelError:
		return "error"
	default:
		return "info"
	}
}

// appendJSONTextValue covers the bench corpus's value shapes only (the
// 12-field event: strings, ints, bools) — not the full adapter type set.
func appendJSONTextValue(dst []byte, value any) []byte {
	switch val := value.(type) {
	case string:
		dst, _ = jsontext.AppendQuote(dst, val)
		return dst
	case int:
		return strconv.AppendInt(dst, int64(val), 10)
	case int64:
		return strconv.AppendInt(dst, val, 10)
	case uint64:
		return strconv.AppendUint(dst, val, 10)
	case float64:
		return jsontext.AppendFloat(dst, val, 64)
	case bool:
		return strconv.AppendBool(dst, val)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			dst, _ = jsontext.AppendQuote(dst, err.Error())
			return dst
		}
		return append(dst, b...)
	}
}

// BenchmarkJSONSinkJsontextComparator races the jsontext backend against
// the forked encoder (BenchmarkJSONSink/write_12_fields) on the same
// event shape.
func BenchmarkJSONSinkJsontextComparator(b *testing.B) {
	sink := &jsontextSink{w: io.Discard}
	fork := hc.NewJSONSink(io.Discard)
	medium := jsonSinkFields12

	b.Run("jsontext_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sink.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
	b.Run("fork_12_fields", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			fork.Write(hc.LevelInfo, "request_completed", medium)
		}
	})
}
