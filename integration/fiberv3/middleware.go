package fiberv3hlog

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/hlog"
	"github.com/happytoolin/hlog/integration/common"
)

// Middleware returns a Fiber v3 middleware that captures one event per request.
func Middleware(cfg hlog.Config) fiber.Handler {
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
				Method:     c.Method(),
				Path:       c.Path(),
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
				_ = errorHandler(c, err)
			}
			err = nil
		}
		return err
	}
}

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
