package main

import (
	"log/slog"
	"os"

	"github.com/happytoolin/hlog"
	slogadapter "github.com/happytoolin/hlog/adapter/slog"
	echohlog "github.com/happytoolin/hlog/integration/echo"
	"github.com/labstack/echo/v4"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	e := echo.New()
	e.Use(echohlog.Middleware(hlog.Config{Sink: sink, SamplingRate: 1}))
	e.GET("/users/:id", func(c echo.Context) error {
		hlog.Add(c.Request().Context(), "router", "echo")
		return c.NoContent(200)
	})

	_ = e.Start(":8103")
}
