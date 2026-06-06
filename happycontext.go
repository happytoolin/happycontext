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
	start := hydrateOperationStart(OperationStart{}, e)
	domain := DomainHTTP
	if start.Domain != "" {
		domain = start.Domain
	}
	message := resolveEventMessage("", domain, snap.message)
	sink.Write(level, message, snap.fields)
	return true
}
