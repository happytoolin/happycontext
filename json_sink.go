package hc

import (
	"context"
	"io"
	"sync"
)

// JSONSink is a first-party sink that writes one canonical JSON line per
// event with a single Write call to the underlying writer — no logger
// dependency required. The line is the record's Encoded() form, byte-
// identical to the v0.6 zerolog-parity sink. A JSONSink is safe for
// concurrent use: encoding happens without a lock, then Write is
// serialized so unsynchronized writers (bytes.Buffer) stay race-clean.
// Write errors are ignored (events are best-effort, like the bridges).
type JSONSink struct {
	mu sync.Mutex
	w  io.Writer
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
	b := rec.Encoded()
	s.mu.Lock()
	_, _ = s.w.Write(b)
	s.mu.Unlock()
}

var _ Sink = (*JSONSink)(nil)
