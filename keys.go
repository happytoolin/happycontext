package hc

// The canonical field keys the library writes: start metadata, HTTP
// request fields, completion annotations, and the error/panic fields.
// Every hc-side write and scan goes through these constants; user code
// may use them with Record.Lookup, CapturedEvent.Lookup, and
// SampleInput.Lookup. The values are a wire contract — pinned by the
// golden and property tests, they must never change.
const (
	KeyOpDomain      = "op.domain"
	KeyOpName        = "op.name"
	KeyOpID          = "op.id"
	KeyOpSource      = "op.source"
	KeyOpAttempt     = "op.attempt"
	KeyOpMaxAttempts = "op.max_attempts"
	KeyOpCode        = "op.code"
	KeyOpOutcome     = "op.outcome"

	KeyHTTPMethod = "http.method"
	KeyHTTPPath   = "http.path"
	KeyHTTPRoute  = "http.route"
	KeyHTTPStatus = "http.status"

	KeyDurationMS = "duration_ms"
	KeyError      = "error"
	KeyPanic      = "panic"

	// KeyJobScheduledAt is written by the worker integration for job
	// metadata that has no op.* equivalent.
	KeyJobScheduledAt = "job.scheduled_at"
)
