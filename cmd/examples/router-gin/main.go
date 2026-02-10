package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	ginhappycontext "github.com/happytoolin/happycontext/integration/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	r := gin.New()
	r.Use(ginhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	r.GET("/users/:id", func(c *gin.Context) {
		ctx := c.Request.Context()
		id := c.Param("id")

		hc.Add(ctx, "router", "gin")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.Add(
			ctx,
			"user", map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request", map[string]any{
				"feature": "profile",
				"tags":    []string{"examples", "router-gin"},
			},
		)
		hc.SetRoute(ctx, "/users/:id")

		if c.Query("debug") == "1" {
			hc.SetLevel(ctx, hc.LevelDebug)
		}
		if level, ok := hc.GetLevel(ctx); ok {
			hc.Add(ctx, "requested_level", level)
		}
		if c.Query("fail") == "1" {
			hc.Error(ctx, errors.New("demo failure"))
			c.Status(500)
			return
		}

		c.Status(200)
	})

	_ = r.Run(":8105")
}
