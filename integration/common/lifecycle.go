// Package common carries the framework-agnostic pieces of the HTTP
// integrations: request start/finalize on the v2 lifecycle plus status
// resolution. Framework middlewares stay thin adapters over these.
package common

import (
	"context"
	"fmt"
	"net/http"

	hc "github.com/happytoolin/happycontext"
)

// StartRequest opens the request lifecycle: it attaches the WAL to the
// request context and records the base HTTP fields. The returned
// operation carries the enriched context (op.Context()).
func StartRequest(baseCtx context.Context, rt *hc.Runtime, method, path string) *hc.Operation {
	op := hc.Start(baseCtx, rt, hc.OperationStart{
		Domain: hc.DomainHTTP,
		Name:   "request",
	})
	hc.Add(op.Context(), "http.method", method, "http.path", path)
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
// hc.Error: End(&err) records it.
func FinalizeRequest(op *hc.Operation, route string, statusCode int, err error, recovered any) {
	if op == nil {
		return
	}
	if route != "" {
		hc.Add(op.Context(), "http.route", route)
		// v0 parity: the operation's name is the resolved route template
		// (last-write-wins keeps this over StartRequest's "request").
		hc.Add(op.Context(), "op.name", route)
	}
	hc.Add(op.Context(), "http.status", statusCode)

	if recovered != nil {
		hc.Add(op.Context(), "panic", PanicField(recovered))
		// The real error (if any) wins over the synthetic panic one.
		if err != nil {
			hc.Error(op.Context(), err)
		} else {
			hc.Error(op.Context(), fmt.Errorf("panic: %v", recovered))
		}
		hc.Add(op.Context(), "op.outcome", string(hc.OutcomePanic))
		_ = op.End(nil)
		return
	}
	if err != nil {
		// No explicit hc.Error here: End(&err) writes the canonical
		// error field itself (annotateOperationFailures). The v0 map
		// folded a double write invisibly; the append-only WAL would
		// keep both entries in Fields().
	}
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
