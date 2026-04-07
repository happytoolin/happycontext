package hc

import "context"

// Commit writes the current event snapshot immediately via sink.
func Commit(ctx context.Context, sink Sink, level Level) bool {
	if sink == nil {
		return false
	}
	e := FromContext(ctx)
	if e == nil {
		return false
	}
	if !IsValidLevel(level) {
		return false
	}
	snap := e.snapshot()
	message := DefaultMessage
	if snap.message != "" {
		message = snap.message
	}
	sink.Write(level, message, snap.fields)
	return true
}
