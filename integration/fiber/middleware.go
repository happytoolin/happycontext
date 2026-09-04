// Package fiberhappycontext provides the Fiber v2 happycontext
// middleware: one canonical event per request, with errors, panics,
// status, and route resolved from the Fiber context.
package fiberhappycontext

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Middleware returns a Fiber v2 middleware that captures one event per request.
func Middleware(rt *hc.Runtime) fiber.Handler {
	if rt == nil {
		return func(c *fiber.Ctx) error {
			return c.Next()
		}
	}

	return func(c *fiber.Ctx) (err error) {
		op := common.StartRequest(c.UserContext(), rt, c.Method(), c.Path())
		c.SetUserContext(op.Context())
		var finalizeErr error

		defer func() {
			recovered := recover()
			routePath := ""
			if route := c.Route(); route != nil && route.Path != "" {
				routePath = route.Path
			}
			status := c.Response().StatusCode()
			responseStarted := status != 0 && (status != http.StatusOK || len(c.Response().Body()) > 0)
			status = common.ResolveStatus(status, finalizeErr, recovered, responseStarted, statusFromFiberError(finalizeErr))
			common.FinalizeRequest(op, routePath, status, finalizeErr, recovered)

			if recovered != nil {
				panic(recovered)
			}
		}()

		err = c.Next()
		finalizeErr = err
		if err != nil {
			if errorHandler := c.App().Config().ErrorHandler; errorHandler != nil {
				if handlerErr := errorHandler(c, err); handlerErr != nil {
					// Error handler failed; capture this as the final error
					finalizeErr = handlerErr
					err = handlerErr
					return err
				}
			}
			err = nil
		}
		return err
	}
}

// statusFromFiberError extracts the HTTP status code from a Fiber error.
// This function is duplicated in the fiberv3 middleware because the
// context types are incompatible between fiber v2 (*fiber.Ctx) and v3 (fiber.Ctx).
// The Error type is compatible, but the middleware signatures differ.
func statusFromFiberError(err error) int {
	if err == nil {
		return 0
	}
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return fiberErr.Code
	}
	return http.StatusInternalServerError
}
