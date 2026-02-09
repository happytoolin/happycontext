package hlog

import "context"

// Commit writes the current event snapshot immediately via sink.
func Commit(ctx context.Context, sink Sink, level string) {
	if sink == nil {
		return
	}
	e := FromContext(ctx)
	if e == nil {
		return
	}
	sink.Write(ctx, level, defaultMessage, e.Snapshot().Fields)
}
