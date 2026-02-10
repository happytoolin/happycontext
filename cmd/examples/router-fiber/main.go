package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	fiberhappycontext "github.com/happytoolin/happycontext/integration/fiber"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	app := fiber.New()
	app.Use(fiberhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		id := c.Params("id")

		hc.Add(ctx, "router", "fiber-v2")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.Add(
			ctx,
			"user", map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request", map[string]any{
				"feature": "profile",
				"tags":    []string{"examples", "router-fiber"},
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
			return c.SendStatus(500)
		}

		return c.SendStatus(200)
	})

	_ = app.Listen(":8107")
}
