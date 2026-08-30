package hc

import (
	"io"
	"sync"
	"time"

	"github.com/happytoolin/happycontext/internal/hcjson"
)

// JSONSink is a first-party sink that writes one canonical JSON line per
// event with a single Write call to the underlying writer — no logger
// dependency required. The field set mirrors the v0.5 zerolog adapter:
// lowercase `level`, the event fields in map order, RFC3339 `time`
// (completion time, second precision, cached per second), and `message`
// last — the same order zerolog's Timestamp hook produces. Empty messages
// fall back to DefaultMessage, unknown levels render as "info", exactly
// like the adapter.
//
// A JSONSink is safe for concurrent use; each Write borrows a pooled
// buffer. Write errors are ignored (events are best-effort, like the
// adapters). The writer must not retain the byte slice passed to Write
// after Write returns — the pooled buffer is reused for the next event.
type JSONSink struct {
	w io.Writer
}

// NewJSONSink returns a JSON sink that writes newline-delimited JSON
// events to w. A nil writer yields a no-op sink.
func NewJSONSink(w io.Writer) *JSONSink {
	return &JSONSink{w: w}
}

// Precomputed level prefixes (zerolog-style): no key encoding, one append.
var (
	jsonLevelPrefixDebug = []byte(`{"level":"debug"`)
	jsonLevelPrefixInfo  = []byte(`{"level":"info"`)
	jsonLevelPrefixWarn  = []byte(`{"level":"warn"`)
	jsonLevelPrefixError = []byte(`{"level":"error"`)
)

func jsonLevelPrefix(level Level) []byte {
	switch level {
	case LevelDebug:
		return jsonLevelPrefixDebug
	case LevelWarn:
		return jsonLevelPrefixWarn
	case LevelError:
		return jsonLevelPrefixError
	default:
		return jsonLevelPrefixInfo
	}
}

var jsonEnc = hcjson.Encoder{}

var jsonSinkBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

// Write implements Sink.
func (s *JSONSink) Write(level Level, message string, fields map[string]any) {
	if s == nil || s.w == nil {
		return
	}

	bp := jsonSinkBufferPool.Get().(*[]byte)
	b := (*bp)[:0]
	b = append(b, jsonLevelPrefix(level)...)
	for k, v := range fields {
		b = jsonEnc.AppendKey(b, k)
		b = appendJSONValue(b, v)
	}
	// The completion-time stamp goes after the fields, matching where
	// zerolog's Timestamp hook emits it, so a user field named "time" is
	// shadowed identically by both sinks.
	b = jsonEnc.AppendKey(b, "time")
	b = hcjson.AppendTimeRFC3339(b, time.Now())
	if message == "" {
		message = DefaultMessage
	}
	b = jsonEnc.AppendKey(b, "message")
	b = jsonEnc.AppendString(b, message)
	b = append(b, '}', '\n')

	_, _ = s.w.Write(b)
	*bp = b
	jsonSinkBufferPool.Put(bp)
}

// appendJSONValue mirrors the zerolog adapter's field type switch so both
// sinks emit the same wire values.
func appendJSONValue(dst []byte, value any) []byte {
	switch val := value.(type) {
	case string:
		return jsonEnc.AppendString(dst, val)
	case int:
		return jsonEnc.AppendInt(dst, val)
	case int8:
		return jsonEnc.AppendInt8(dst, val)
	case int16:
		return jsonEnc.AppendInt16(dst, val)
	case int32:
		return jsonEnc.AppendInt32(dst, val)
	case int64:
		return jsonEnc.AppendInt64(dst, val)
	case uint:
		return jsonEnc.AppendUint(dst, val)
	case uint8:
		return jsonEnc.AppendUint8(dst, val)
	case uint16:
		return jsonEnc.AppendUint16(dst, val)
	case uint32:
		return jsonEnc.AppendUint32(dst, val)
	case uint64:
		return jsonEnc.AppendUint64(dst, val)
	case float32:
		return jsonEnc.AppendFloat32(dst, val, -1)
	case float64:
		return jsonEnc.AppendFloat64(dst, val, -1)
	case bool:
		return jsonEnc.AppendBool(dst, val)
	case time.Time:
		return jsonEnc.AppendTime(dst, val, time.RFC3339)
	case time.Duration:
		// zerolog defaults: float milliseconds, shortest round-trip
		return jsonEnc.AppendDuration(dst, val, time.Millisecond, false, -1)
	case error:
		return jsonEnc.AppendString(dst, val.Error())
	default:
		return jsonEnc.AppendInterface(dst, val)
	}
}

var _ Sink = (*JSONSink)(nil)
