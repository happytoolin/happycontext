package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	echohappycontext "github.com/happytoolin/happycontext/integration/echo"
	"github.com/labstack/echo/v4"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	e := echo.New()
	e.Use(echohappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	e.GET("/users/:id", func(c echo.Context) error {
		ctx := c.Request().Context()
		id := c.Param("id")

		hc.Add(ctx, "router", "echo")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.AddMap(ctx, map[string]any{
			"user": map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request": map[string]any{
				"feature": "profile",
				"tags":    []string{"examples", "router-echo"},
			},
		})
		hc.SetRoute(ctx, "/users/:id")

		if c.QueryParam("debug") == "1" {
			hc.SetLevel(ctx, hc.LevelDebug)
		}
		if level, ok := hc.GetLevel(ctx); ok {
			hc.Add(ctx, "requested_level", level)
		}
		if c.QueryParam("fail") == "1" {
			hc.Error(ctx, errors.New("demo failure"))
			return c.NoContent(500)
		}

		return c.NoContent(200)
	})

	_ = e.Start(":8106")
}
