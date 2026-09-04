package hc

import "context"

// Sink receives finalized request events as read-only records, following
// the slog.Handler.Handle shape. Implementations must be safe for
// concurrent use (amendment 2) and must not retain the record or its
// bytes past the call (copy anything you keep; amendment 9).
type Sink interface {
	Write(ctx context.Context, rec *Record)
}
