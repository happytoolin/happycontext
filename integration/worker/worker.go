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

func addJobFields(ctx context.Context, meta JobMeta) {
	kv := []any{
		"job.name", meta.Name,
		"job.id", meta.ID,
		"job.queue", meta.Queue,
		"job.attempt", meta.Attempt,
		"job.max_attempts", meta.MaxAttempts,
	}
	if !meta.ScheduledAt.IsZero() {
		kv = append(kv, "job.scheduled_at", meta.ScheduledAt.UTC())
	}
	hc.Add(ctx, kv[0].(string), kv[1], kv[2:]...)
}
