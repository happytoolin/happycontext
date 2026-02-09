package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	fiberhlog "github.com/happytoolin/hlog/integration/fiber"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	app := fiber.New()
	app.Use(fiberhlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		hlog.Add(c.UserContext(), "router", "fiber-v2")
		return c.SendStatus(200)
	})

	_ = app.Listen(":8104")
}
