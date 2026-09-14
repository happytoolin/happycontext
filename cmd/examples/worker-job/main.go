package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	"github.com/happytoolin/unolog/integration/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)
	rt := unolog.MustCompile(unolog.Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	meta := worker.JobMeta{
		Name:        "billing.reconcile",
		ID:          "job_8472",
		Queue:       "nightly",
		Attempt:     1,
		MaxAttempts: 3,
		ScheduledAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := runJob(context.Background(), rt, meta); err != nil {
		logger.Error("job failed", "error", err)
	}
}

func runJob(ctx context.Context, rt *unolog.Runtime, meta worker.JobMeta) (err error) {
	op := worker.Start(ctx, rt, meta)
	defer op.End(&err)

	unolog.Add(op.Context(), "tenant", "enterprise", "worker", "billing")
	return nil
}
