package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	workerhappycontext "github.com/happytoolin/happycontext/integration/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	meta := workerhappycontext.JobMeta{
		Name:        "billing.reconcile",
		ID:          "job_8472",
		Queue:       "nightly",
		Attempt:     1,
		MaxAttempts: 3,
		ScheduledAt: time.Now().UTC().Truncate(time.Second),
	}

	ctx, event := workerhappycontext.Start(context.Background(), meta)
	hc.Add(ctx, "tenant", "enterprise", "worker", "billing")

	_ = workerhappycontext.Finish(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}, ctx, event, meta, nil, nil)
}
