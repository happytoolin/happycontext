package hc

import (
	"context"
	"io"
)

// JSONSink is a first-party sink that writes one canonical JSON line per
// event with a single Write call to the underlying writer — no logger
// dependency required. The line is the record's Encoded() form, byte-
// identical to the v0.6 zerolog-parity sink. A JSONSink is safe for
// concurrent use; Write errors are ignored (events are best-effort, like
// the bridges).
type JSONSink struct {
	w io.Writer
}

// NewJSONSink returns a JSON sink writing newline-delimited canonical
// events to w. A nil writer yields a no-op sink.
func NewJSONSink(w io.Writer) *JSONSink {
	return &JSONSink{w: w}
}

// Write implements Sink.
func (s *JSONSink) Write(_ context.Context, rec *Record) {
	if s == nil || s.w == nil || rec == nil {
		return
	}
	_, _ = s.w.Write(rec.Encoded())
}

var _ Sink = (*JSONSink)(nil)
