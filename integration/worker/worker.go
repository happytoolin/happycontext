package workerhappycontext

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

// Start initializes a worker operation handle.
func Start(ctx context.Context, meta JobMeta) *hc.Operation {
	op := hc.StartOperation(ctx, hc.OperationStart{
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

// Finish finalizes and writes the worker operation event.
func Finish(cfg hc.Config, op *hc.Operation, err error, recovered any) bool {
	if op == nil {
		return false
	}
	return op.Finish(cfg, hc.OperationResult{
		Err:       err,
		Recovered: recovered,
	})
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
