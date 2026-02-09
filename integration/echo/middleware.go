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
	cfg = common.NormalizeConfig(cfg)
	if cfg.Sink == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				return next(c)
			}
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			ctx, event := common.StartRequest(c.Request().Context(), c.Request().Method, c.Request().URL.Path)
			c.SetRequest(c.Request().WithContext(ctx))
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
				common.FinalizeRequest(cfg, common.FinalizeInput{
					Ctx:        ctx,
					Event:      event,
					Method:     c.Request().Method,
					Path:       c.Request().URL.Path,
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
