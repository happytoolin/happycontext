package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	hc "github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	workerhappycontext "github.com/happytoolin/happycontext/integration/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)
	cfg := hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}

	meta := workerhappycontext.JobMeta{
		Name:        "billing.reconcile",
		ID:          "job_8472",
		Queue:       "nightly",
		Attempt:     1,
		MaxAttempts: 3,
		ScheduledAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := runJob(context.Background(), cfg, meta); err != nil {
		logger.Error("job failed", "error", err)
	}
}

func runJob(ctx context.Context, cfg hc.Config, meta workerhappycontext.JobMeta) (err error) {
	op := workerhappycontext.Start(ctx, meta)
	defer op.End(cfg, &err)

	hc.Add(op.Context(), "tenant", "enterprise", "worker", "billing")
	return nil
}
