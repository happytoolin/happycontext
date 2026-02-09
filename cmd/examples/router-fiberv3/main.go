package main

import (
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	fiberv3hlog "github.com/happytoolin/hlog/integration/fiberv3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	app := fiber.New()
	app.Use(fiberv3hlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/users/:id", func(c fiber.Ctx) error {
		hlog.Add(c.Context(), "router", "fiber-v3")
		return c.SendStatus(200)
	})

	_ = app.Listen(":8105")
}
