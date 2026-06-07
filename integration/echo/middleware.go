package echohappycontext

import (
	"errors"
	"net/http"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
	"github.com/labstack/echo/v4"
)

// Middleware returns an Echo middleware that captures one event per request.
func Middleware(cfg hc.Config) echo.MiddlewareFunc {
	prepared := common.PrepareRequestConfig(cfg)
	if prepared.Config.Sink == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				return next(c)
			}
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			req := c.Request()
			ctx, event := common.StartRequest(req.Context(), req.Method, req.URL.Path)
			oldCtx, swappedCtx := common.SwapRequestContextUnsafe(req, ctx)
			if !swappedCtx {
				c.SetRequest(req.WithContext(ctx))
			}
			var finalizeErr error

			defer func() {
				recovered := recover()
				route := c.Path()
				status := common.ResolveStatus(
					c.Response().Status,
					finalizeErr,
					recovered,
					c.Response().Committed,
					statusFromEchoError(finalizeErr),
				)
				if swappedCtx {
					_, _ = common.SwapRequestContextUnsafe(req, oldCtx)
				}
				common.FinalizePreparedRequest(prepared, common.FinalizeInput{
					Ctx:        ctx,
					Event:      event,
					Route:      route,
					StatusCode: status,
					Err:        finalizeErr,
					Recovered:  recovered,
				})

				if recovered != nil {
					panic(recovered)
				}
			}()

			err = next(c)
			finalizeErr = err
			if err != nil && !c.Response().Committed {
				c.Error(err)
				err = nil
			}
			return err
		}
	}
}

func statusFromEchoError(err error) int {
	if err == nil {
		return 0
	}
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code
	}
	return http.StatusInternalServerError
}
