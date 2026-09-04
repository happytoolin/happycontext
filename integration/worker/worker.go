// Package workerhc provides the background-job happycontext lifecycle:
// Start opens a job operation from JobMeta and returns the deferred-End
// handle.
package workerhc

import (
	"context"
	"time"

	hc "github.com/happytoolin/happycontext"
)

// JobMeta describes background job execution metadata.
type JobMeta struct {
	Name        string
	ID          string
	Queue       string
	Attempt     int
	MaxAttempts int
	ScheduledAt time.Time
}

// Start initializes a worker operation handle. rt comes from
// hc.Compile/MustCompile; a nil *hc.Runtime runs the operation with no
// emission. End the operation with the deferred-error idiom:
//
//	func run(ctx context.Context) (err error) {
//		op := workerhc.Start(ctx, rt, meta)
//		defer op.End(&err)
//		...
//	}
func Start(ctx context.Context, rt *hc.Runtime, meta JobMeta) *hc.Operation {
	op := hc.Start(ctx, rt, hc.OperationStart{
		Domain:      hc.DomainJob,
		Name:        meta.Name,
		ID:          meta.ID,
		Source:      meta.Queue,
		Attempt:     meta.Attempt,
		MaxAttempts: meta.MaxAttempts,
	})
	addJobFields(op.Context(), meta)
	return op
}

// addJobFields records only what op.* does not already carry: the
// mirrors (name/id/queue/attempt/max_attempts) were dropped with the
// canonical-field pass; scheduled_at has no op.* equivalent.
func addJobFields(ctx context.Context, meta JobMeta) {
	if !meta.ScheduledAt.IsZero() {
		hc.Add(ctx, "job.scheduled_at", meta.ScheduledAt.UTC())
	}
}
