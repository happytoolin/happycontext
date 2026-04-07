package fiberv3hc

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Middleware returns a Fiber v3 middleware that captures one event per request.
func Middleware(cfg hc.Config) fiber.Handler {
	cfg = common.NormalizeConfig(cfg)
	if cfg.Sink == nil {
		return func(c fiber.Ctx) error {
			return c.Next()
		}
	}

	return func(c fiber.Ctx) (err error) {
		ctx, event := common.StartRequest(c.Context(), c.Method(), c.Path())
		c.SetContext(ctx)
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
			common.FinalizeRequest(cfg, common.FinalizeInput{
				Ctx:        ctx,
				Event:      event,
				Route:      routePath,
				StatusCode: status,
				Err:        finalizeErr,
				Recovered:  recovered,
			})

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
				}
			}
			err = nil
		}
		return err
	}
}

// statusFromFiberError extracts the HTTP status code from a Fiber error.
// This function is duplicated from the fiber v2 middleware because the
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
