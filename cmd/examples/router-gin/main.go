package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	ugin "github.com/happytoolin/unolog/integration/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	r := gin.New()
	r.Use(ugin.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	r.GET("/users/:id", func(c *gin.Context) {
		ctx := c.Request.Context()
		id := c.Param("id")

		unolog.Add(ctx, "router", "gin")
		unolog.Add(
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
		unolog.SetRoute(ctx, "/users/:id")

		if c.Query("debug") == "1" {
			unolog.SetLevel(ctx, unolog.LevelDebug)
			unolog.Add(ctx, "requested_level", unolog.LevelDebug)
		}
		if c.Query("fail") == "1" {
			unolog.Error(ctx, errors.New("demo failure"))
			c.Status(500)
			return
		}

		c.Status(200)
	})

	_ = r.Run(":8105")
}
