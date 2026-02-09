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
	if !isValidLevel(level) {
		return false
	}
	sink.Write(level, defaultMessage, e.Snapshot().Fields)
	return true
}
