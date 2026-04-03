package common

import (
	"context"
	"net/http"

	hc "github.com/happytoolin/happycontext"
)

// FinalizeInput contains request data required for finalization.
type FinalizeInput struct {
	Ctx        context.Context
	Event      *hc.Event
	Method     string
	Path       string
	Route      string
	StatusCode int
	Err        error
	Recovered  any
}

// StartRequest initializes request context and base HTTP fields.
func StartRequest(baseCtx context.Context, method, path string) (context.Context, *hc.Event) {
	ctx, event := hc.BeginOperation(baseCtx, hc.OperationStart{
		Domain: hc.DomainHTTP,
		Name:   "request",
	})
	hc.Add(ctx, "http.method", method, "http.path", path)
	return ctx, event
}

// FinalizeRequest computes status/level/sampling and writes the final snapshot.
func FinalizeRequest(cfg hc.Config, in FinalizeInput) {
	if in.Route != "" {
		hc.SetRoute(in.Ctx, in.Route)
	}
	hc.Add(in.Ctx, "http.method", in.Method, "http.path", in.Path, "http.status", in.StatusCode)

	name := "request"
	if in.Route != "" {
		name = in.Route
	} else if in.Method != "" {
		name = in.Method
	}

	hc.FinishOperation(cfg, hc.OperationFinish{
		Ctx:   in.Ctx,
		Event: in.Event,
		Start: hc.OperationStart{
			Domain: hc.DomainHTTP,
			Name:   name,
		},
		Code:      in.StatusCode,
		Err:       in.Err,
		Recovered: in.Recovered,
	})
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
