package workerhappycontext

import (
	"context"
	"time"

	"github.com/happytoolin/happycontext"
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

// Start initializes happycontext operation/event fields for a worker job.
func Start(ctx context.Context, meta JobMeta) (context.Context, *hc.Event) {
	ctx, event := hc.BeginOperation(ctx, hc.OperationStart{
		Domain:      hc.DomainJob,
		Name:        meta.Name,
		ID:          meta.ID,
		Source:      meta.Queue,
		Attempt:     meta.Attempt,
		MaxAttempts: meta.MaxAttempts,
	})
	addJobFields(ctx, meta)
	return ctx, event
}

// Finish finalizes and writes the worker operation event.
func Finish(cfg hc.Config, ctx context.Context, event *hc.Event, meta JobMeta, err error, recovered any) bool {
	addJobFields(ctx, meta)
	return hc.FinishOperation(cfg, hc.OperationFinish{
		Ctx:   ctx,
		Event: event,
		Start: hc.OperationStart{
			Domain:      hc.DomainJob,
			Name:        meta.Name,
			ID:          meta.ID,
			Source:      meta.Queue,
			Attempt:     meta.Attempt,
			MaxAttempts: meta.MaxAttempts,
		},
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
