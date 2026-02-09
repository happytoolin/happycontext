package common

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/happytoolin/happycontext"
)

// FinalizeInput contains request data required for finalization.
type FinalizeInput struct {
	Ctx        context.Context
	Event      *happycontext.Event
	Method     string
	Path       string
	Route      string
	StatusCode int
	Err        error
	Recovered  any
}

// StartRequest initializes request context and base HTTP fields.
func StartRequest(baseCtx context.Context, method, path string) (context.Context, *happycontext.Event) {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, event := happycontext.NewContext(baseCtx)
	happycontext.Add(ctx, "http.method", method)
	happycontext.Add(ctx, "http.path", path)
	return ctx, event
}

// FinalizeRequest computes status/level/sampling and writes the final snapshot.
func FinalizeRequest(cfg happycontext.Config, in FinalizeInput) {
	if cfg.Sink == nil || in.Event == nil || in.Ctx == nil {
		return
	}

	annotateFailures(in.Ctx, in.Err, in.Recovered)
	if in.Route != "" {
		happycontext.SetRoute(in.Ctx, in.Route)
	}

	duration := annotateTiming(in.Ctx, in.Event, in.StatusCode)
	hasError := in.Event.HasError() || in.StatusCode >= 500
	level := resolveLevel(in.Ctx, hasError)
	if !shouldWriteEvent(happycontext.SampleInput{
		Method:     in.Method,
		Path:       in.Path,
		HasError:   hasError,
		StatusCode: in.StatusCode,
		Duration:   duration,
		Rate:       cfg.SamplingRate,
	}) {
		return
	}
	snapshot := in.Event.Snapshot()
	cfg.Sink.Write(in.Ctx, level, cfg.Message, snapshot.Fields)
}

func annotateFailures(ctx context.Context, err error, recovered any) {
	if recovered != nil {
		happycontext.Add(ctx, "panic", map[string]any{
			"type":  fmt.Sprintf("%T", recovered),
			"value": fmt.Sprint(recovered),
		})
		happycontext.Error(ctx, fmt.Errorf("panic: %v", recovered))
	}
	if err != nil {
		happycontext.Error(ctx, err)
	}
}

func annotateTiming(ctx context.Context, event *happycontext.Event, statusCode int) time.Duration {
	duration := time.Since(event.StartTime())
	happycontext.Add(ctx, "duration_ms", duration.Milliseconds())
	happycontext.Add(ctx, "http.status", statusCode)
	return duration
}

func resolveLevel(ctx context.Context, hasError bool) string {
	autoLevel := happycontext.LevelInfo
	if hasError {
		autoLevel = happycontext.LevelError
	}
	requestedLevel, hasRequestedLevel := happycontext.GetLevel(ctx)
	return MergeLevelWithFloor(autoLevel, requestedLevel, hasRequestedLevel)
}

// ResolveStatus determines the final HTTP status to log.
func ResolveStatus(currentStatus int, err error, recovered any, responseStarted bool, errorStatus int) int {
	if recovered != nil && !responseStarted {
		return http.StatusInternalServerError
	}

	if err != nil && !responseStarted {
		if errorStatus >= 400 {
			return errorStatus
		}
		return http.StatusInternalServerError
	}

	if currentStatus == 0 {
		return http.StatusOK
	}

	return currentStatus
}
