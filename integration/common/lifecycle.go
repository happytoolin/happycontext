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
	Route      string
	StatusCode int
	Err        error
	Recovered  any
}

// StartRequest initializes request context and base HTTP fields.
func StartRequest(baseCtx context.Context, method, path string) (context.Context, *hc.Event) {
	ctx, event := hc.NewContext(baseCtx)
	event.Add2Strings("http.method", method, "http.path", path)
	return ctx, event
}

// FinalizeRequest computes status/level/sampling and writes the final snapshot.
func FinalizeRequest(cfg hc.Config, in FinalizeInput) {
	FinalizePreparedRequest(PrepareRequestConfig(cfg), in)
}

// FinalizePreparedRequest computes status/level/sampling and writes the final
// snapshot using config prepared once at middleware construction.
func FinalizePreparedRequest(prepared PreparedRequestConfig, in FinalizeInput) {
	if in.Ctx != nil && in.Event != nil {
		if in.Route != "" {
			in.Event.SetRoute(in.Route)
		}
		in.Event.AddInt("http.status", in.StatusCode)
	}

	name := "request"
	if in.Route != "" {
		name = in.Route
	}

	hc.FinishOperation(prepared.Config, hc.OperationFinish{
		Ctx:   in.Ctx,
		Event: in.Event,
		Start: hc.OperationStart{
			Domain: hc.DomainHTTP,
			Name:   name,
		},
		StartComplete: true,
		Code:          in.StatusCode,
		Err:           in.Err,
		Recovered:     in.Recovered,
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
