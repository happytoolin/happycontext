package hc

import (
	"context"
)

// contextKey carries the request's WAL handle. The accessors are
// unexported: the in-flight WAL is written by the hc.Add helpers and
// read once at End — nothing reads it mid-flight by design.
type contextKey struct{}

func eventFromContext(ctx context.Context) *walRef {
	if ctx == nil {
		return nil
	}
	ref, _ := ctx.Value(contextKey{}).(*walRef)
	return ref
}

// Add records one or more fields on the request's event. With no event
// attached (or after End) it is a silent no-op, exactly like the slog
// helper family.
//
// Additional pairs may be passed variadically: Add(ctx, "a", 1, "b", 2).
// Every pair key must be a string; malformed tails are skipped.
//
// The request goroutine is the sole writer: passing the enriched
// context to child goroutines is fine, but hc.Add from a child
// goroutine racing the request's own writes is a data race — fan
// results back over a channel and Add them on the request goroutine.
func Add(ctx context.Context, key string, value any, kv ...any) {
	ref := eventFromContext(ctx)
	if ref == nil {
		return
	}
	if len(kv) == 0 {
		ref.ev.append(ref.gen, fieldOf(key, value)) // single-pair fast path
		return
	}
	ref.ev.addKV(ref, key, value, kv...)
}

// Error records err on the request's event as the structured canonical
// error field.
func Error(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if ref := eventFromContext(ctx); ref != nil {
		ref.ev.setError(ref, err)
	}
}

// SetMessage sets a per-event final message override. An empty message
// is treated as unset during finalization.
func SetMessage(ctx context.Context, msg string) {
	if ref := eventFromContext(ctx); ref != nil {
		ref.ev.setMessage(ref, msg)
	}
}

// SetRoute sets a normalized route template (http.route) on the event.
func SetRoute(ctx context.Context, route string) {
	if ref := eventFromContext(ctx); ref != nil {
		ref.ev.setRoute(ref, route)
	}
}

// SetLevel requests a severity floor for the final event; the emitted
// level is never lower than the request.
func SetLevel(ctx context.Context, level Level) {
	if ref := eventFromContext(ctx); ref != nil {
		ref.ev.setLevel(ref, level)
	}
}
