// Package echohappycontext provides the Echo happycontext middleware:
// one canonical event per request, with errors, panics, status, and
// route resolved from the Echo context.
package echohappycontext

import (
	"errors"
	"net/http"

	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
	"github.com/labstack/echo/v4"
)

// Middleware returns an Echo middleware that captures one event per
// request. rt comes from hc.Compile/MustCompile; nil is a passthrough.
func Middleware(rt *hc.Runtime) echo.MiddlewareFunc {
	if rt == nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				return next(c)
			}
		}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			op := common.StartRequest(c.Request().Context(), rt, c.Request().Method, c.Request().URL.Path)
			c.SetRequest(c.Request().WithContext(op.Context()))
			var finalizeErr error

			defer func() {
				recovered := recover()
				route := c.Path()
				status := common.ResolveStatus(common.StatusInput{
					Committed:       c.Response().Status,
					Err:             finalizeErr,
					Recovered:       recovered,
					ResponseStarted: c.Response().Committed,
					ErrorStatus:     statusFromEchoError(finalizeErr),
				})
				common.FinalizeRequest(op, route, status, finalizeErr, recovered)

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
