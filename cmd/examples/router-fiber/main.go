package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/unolog"
	uslog "github.com/happytoolin/unolog/adapter/slog"
	ufiber "github.com/happytoolin/unolog/integration/fiber"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := uslog.New(logger)

	app := fiber.New()
	app.Use(ufiber.Middleware(unolog.MustCompile(unolog.Config{Sink: sink, SamplingRate: 1})))
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		id := c.Params("id")

		unolog.Add(ctx, "router", "fiber-v2")
		unolog.Add(
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
		unolog.SetRoute(ctx, "/users/:id")

		if c.Query("debug") == "1" {
			unolog.SetLevel(ctx, unolog.LevelDebug)
			unolog.Add(ctx, "requested_level", unolog.LevelDebug)
		}
		if c.Query("fail") == "1" {
			unolog.Error(ctx, errors.New("demo failure"))
			return c.SendStatus(500)
		}

		return c.SendStatus(200)
	})

	_ = app.Listen(":8107")
}
