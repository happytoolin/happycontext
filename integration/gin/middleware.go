package ginhappycontext

import (
	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Middleware returns a Gin middleware that captures one event per request.
func Middleware(cfg hc.Config) gin.HandlerFunc {
	prepared := common.PrepareRequestConfig(cfg)
	if prepared.Config.Sink == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		req := c.Request
		ctx, event := common.StartRequest(req.Context(), req.Method, req.URL.Path)
		oldCtx, swappedCtx := common.SwapRequestContextUnsafe(req, ctx)
		if !swappedCtx {
			c.Request = req.WithContext(ctx)
		}

		defer func() {
			recovered := recover()
			var err error
			if len(c.Errors) > 0 {
				if last := c.Errors.Last(); last != nil {
					err = last.Err
				}
			}
			status := common.ResolveStatus(c.Writer.Status(), err, recovered, c.Writer.Written(), 0)
			if swappedCtx {
				_, _ = common.SwapRequestContextUnsafe(req, oldCtx)
			}
			common.FinalizePreparedRequest(prepared, common.FinalizeInput{
				Ctx:        ctx,
				Event:      event,
				Route:      c.FullPath(),
				StatusCode: status,
				Err:        err,
				Recovered:  recovered,
			})

			if recovered != nil {
				panic(recovered)
			}
		}()

		c.Next()
	}
}
