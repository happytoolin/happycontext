package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	workerhappycontext "github.com/happytoolin/happycontext/integration/worker"
)

func TestWorkerJobExecution(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
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

	t.Run("successful job execution", func(t *testing.T) {
		buf.Reset()
		err := runJob(context.Background(), cfg, meta)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "billing.reconcile") {
			t.Error("expected log output to contain job name")
		}
		if !strings.Contains(output, "job_8472") {
			t.Error("expected log output to contain job ID")
		}
		if !strings.Contains(output, "worker") {
			t.Error("expected log output to contain worker field")
		}
	})

	t.Run("job with different metadata", func(t *testing.T) {
		buf.Reset()
		customMeta := workerhappycontext.JobMeta{
			Name:        "email.send",
			ID:          "job_9999",
			Queue:       "high-priority",
			Attempt:     2,
			MaxAttempts: 5,
			ScheduledAt: time.Now().UTC(),
		}

		err := runJob(context.Background(), cfg, customMeta)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "email.send") {
			t.Error("expected log output to contain job name 'email.send'")
		}
	})
}

func TestWorkerJobMetaValidation(t *testing.T) {
	tests := []struct {
		name string
		meta workerhappycontext.JobMeta
	}{
		{
			name: "minimal metadata",
			meta: workerhappycontext.JobMeta{
				Name: "minimal.job",
				ID:   "job_min",
			},
		},
		{
			name: "full metadata",
			meta: workerhappycontext.JobMeta{
				Name:        "full.job",
				ID:          "job_full",
				Queue:       "default",
				Attempt:     1,
				MaxAttempts: 3,
				ScheduledAt: time.Now(),
			},
		},
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	sink := slogadapter.New(logger)
	cfg := hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			err := runJob(context.Background(), cfg, tt.meta)
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, tt.meta.Name) {
				t.Errorf("expected log output to contain job name %q", tt.meta.Name)
			}
		})
	}
}
