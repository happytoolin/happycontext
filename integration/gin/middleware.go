// Package ginhappycontext provides the Gin happycontext middleware: one
// canonical event per request, with errors, panics, status, and route
// resolved from the Gin context.
package ginhappycontext

import (
	"github.com/gin-gonic/gin"
	hc "github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Middleware returns a Gin middleware that captures one event per
// request. rt comes from hc.Compile/MustCompile; nil is a passthrough.
func Middleware(rt *hc.Runtime) gin.HandlerFunc {
	if rt == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		op := common.StartRequest(c.Request.Context(), rt, c.Request.Method, c.Request.URL.Path)
		c.Request = c.Request.WithContext(op.Context())

		defer func() {
			recovered := recover()
			var err error
			if len(c.Errors) > 0 {
				if last := c.Errors.Last(); last != nil {
					err = last.Err
				}
			}
			status := common.ResolveStatus(c.Writer.Status(), err, recovered, c.Writer.Written(), 0)
			common.FinalizeRequest(op, c.FullPath(), status, err, recovered)

			if recovered != nil {
				panic(recovered)
			}
		}()

		c.Next()
	}
}
