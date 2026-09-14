// Package common carries the framework-agnostic pieces of the HTTP
// integrations: request start/finalize on the v2 lifecycle plus status
// resolution. Framework middlewares stay thin adapters over these.
package flow

import (
	"context"
	"fmt"
	"net/http"

	"github.com/happytoolin/unolog"
)

// StartRequest opens the request lifecycle: it attaches the WAL to the
// request context and records the base HTTP fields. The returned
// operation carries the enriched context (op.Context()).
func StartRequest(baseCtx context.Context, rt *unolog.Runtime, method, path string) *unolog.Operation {
	op := unolog.Start(baseCtx, rt, unolog.OperationStart{
		Domain: unolog.DomainHTTP,
		Name:   "request",
	})
	unolog.Add(op.Context(), unolog.KeyHTTPMethod, method, unolog.KeyHTTPPath, path)
	return op
}

// FinalizeRequest records the resolved route and status, then commits
// the single event via op.End.
//
// recovered is the panic the middleware's defer captured (nil in the
// normal case). Because the middleware — not End — performed the
// recover, the panic reaches the event through the public write API:
// the canonical panic field, an error field carrying "panic: …", and
// an explicit op.outcome=panic (End is called with a nil error so the
// explicit outcome wins). The resulting wire fields are identical to
// End's own panic handling. The non-panic error path needs no explicit
// unolog.Error: End(&err) records it.
func FinalizeRequest(op *unolog.Operation, route string, statusCode int, err error, recovered any) {
	if op == nil {
		return
	}
	if route != "" {
		// One ctx lookup for all three canonical writes (v0 parity:
		// op.name is the resolved route template, last-write-wins over
		// StartRequest's "request").
		unolog.Add(op.Context(), unolog.KeyHTTPRoute, route, unolog.KeyOpName, route, unolog.KeyHTTPStatus, statusCode)
	} else {
		unolog.Add(op.Context(), unolog.KeyHTTPStatus, statusCode)
	}

	if recovered != nil {
		unolog.Add(op.Context(), unolog.KeyPanic, PanicField(recovered))
		// The real error (if any) wins over the synthetic panic one.
		if err != nil {
			unolog.Error(op.Context(), err)
		} else {
			unolog.Error(op.Context(), fmt.Errorf("panic: %v", recovered))
		}
		unolog.Add(op.Context(), unolog.KeyOpOutcome, string(unolog.OutcomePanic))
		_ = op.End(nil)
		return
	}
	// No explicit unolog.Error on the error path: End(&err) writes the
	// canonical error field itself (annotateOperationFailures).
	_ = op.End(&err)
}

// PanicField builds the canonical panic field value (type + value),
// matching the core's structured panic shape.
func PanicField(recovered any) map[string]any {
	return map[string]any{
		"type":  fmt.Sprintf("%T", recovered),
		"value": fmt.Sprint(recovered),
	}
}

// StatusInput carries what a middleware knows when it finalizes: the
// status the framework writer currently holds, the terminal error and
// recovered panic, whether the response already started, and the HTTP
// status the error implies (0 if none).
type StatusInput struct {
	Committed       int
	Err             error
	Recovered       any
	ResponseStarted bool
	ErrorStatus     int
}

// ResolveStatus determines the final HTTP status to log from in:
// panics and handler errors before the response started resolve to a
// 5xx (the error's own status when it carries one), an unwritten
// writer resolves to 200, and a committed status stands.
func ResolveStatus(in StatusInput) int {
	if in.Recovered != nil && !in.ResponseStarted {
		return http.StatusInternalServerError
	}

	if in.Err != nil && !in.ResponseStarted {
		if in.ErrorStatus >= 400 {
			return in.ErrorStatus
		}
		return http.StatusInternalServerError
	}

	if in.Committed == 0 {
		return http.StatusOK
	}

	return in.Committed
}
