package ginhappycontext

import (
	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
	"github.com/happytoolin/happycontext/integration/common"
)

// Middleware returns a Gin middleware that captures one event per request.
func Middleware(cfg hc.Config) gin.HandlerFunc {
	cfg = common.NormalizeConfig(cfg)
	if cfg.Sink == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		ctx, event := common.StartRequest(c.Request.Context(), c.Request.Method, c.Request.URL.Path)
		c.Request = c.Request.WithContext(ctx)

		defer func() {
			recovered := recover()
			var err error
			if len(c.Errors) > 0 {
				err = c.Errors.Last()
			}
			status := common.ResolveStatus(c.Writer.Status(), err, recovered, c.Writer.Written(), 0)
			common.FinalizeRequest(cfg, common.FinalizeInput{
				Ctx:        ctx,
				Event:      event,
				Method:     c.Request.Method,
				Path:       c.Request.URL.Path,
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
