package common

import (
	"context"
	"net/http"

	hc "github.com/happytoolin/happycontext"
)

// Pre-boxed values for fixed HTTP strings and common status codes. Boxing an
// int >= 256 or any string into the event's map[string]any allocates, so the
// fixed set is boxed once here.
var (
	methodGetAny     = any(http.MethodGet)
	methodHeadAny    = any(http.MethodHead)
	methodPostAny    = any(http.MethodPost)
	methodPutAny     = any(http.MethodPut)
	methodPatchAny   = any(http.MethodPatch)
	methodDeleteAny  = any(http.MethodDelete)
	methodConnectAny = any(http.MethodConnect)
	methodOptionsAny = any(http.MethodOptions)
	methodTraceAny   = any(http.MethodTrace)

	status300Any = any(300)
	status301Any = any(301)
	status302Any = any(302)
	status303Any = any(303)
	status304Any = any(304)
	status307Any = any(307)
	status308Any = any(308)
	status400Any = any(400)
	status401Any = any(401)
	status403Any = any(403)
	status404Any = any(404)
	status405Any = any(405)
	status409Any = any(409)
	status410Any = any(410)
	status418Any = any(418)
	status422Any = any(422)
	status429Any = any(429)
	status500Any = any(500)
	status501Any = any(501)
	status502Any = any(502)
	status503Any = any(503)
	status504Any = any(504)
)

func methodAny(method string) any {
	switch method {
	case http.MethodGet:
		return methodGetAny
	case http.MethodHead:
		return methodHeadAny
	case http.MethodPost:
		return methodPostAny
	case http.MethodPut:
		return methodPutAny
	case http.MethodPatch:
		return methodPatchAny
	case http.MethodDelete:
		return methodDeleteAny
	case http.MethodConnect:
		return methodConnectAny
	case http.MethodOptions:
		return methodOptionsAny
	case http.MethodTrace:
		return methodTraceAny
	default:
		return method
	}
}

func statusAny(code int) any {
	switch code {
	case 300:
		return status300Any
	case 301:
		return status301Any
	case 302:
		return status302Any
	case 303:
		return status303Any
	case 304:
		return status304Any
	case 307:
		return status307Any
	case 308:
		return status308Any
	case 400:
		return status400Any
	case 401:
		return status401Any
	case 403:
		return status403Any
	case 404:
		return status404Any
	case 405:
		return status405Any
	case 409:
		return status409Any
	case 410:
		return status410Any
	case 418:
		return status418Any
	case 422:
		return status422Any
	case 429:
		return status429Any
	case 500:
		return status500Any
	case 501:
		return status501Any
	case 502:
		return status502Any
	case 503:
		return status503Any
	case 504:
		return status504Any
	default:
		return code
	}
}

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
	ctx, event := hc.BeginOperation(baseCtx, hc.OperationStart{
		Domain: hc.DomainHTTP,
		Name:   "request",
	})
	hc.Add(ctx, "http.method", methodAny(method), "http.path", path)
	return ctx, event
}

// FinalizeRequest computes status/level/sampling and writes the final snapshot.
func FinalizeRequest(cfg hc.Config, in FinalizeInput) {
	if in.Route != "" {
		hc.Add(in.Ctx, "http.route", in.Route, "http.status", statusAny(in.StatusCode))
	} else {
		hc.Add(in.Ctx, "http.status", statusAny(in.StatusCode))
	}

	name := "request"
	if in.Route != "" {
		name = in.Route
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
