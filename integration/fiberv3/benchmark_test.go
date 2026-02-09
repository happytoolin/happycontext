package fiberv3happycontext

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/happytoolin/happycontext"
)

type discardSink struct{}

func (discardSink) Write(hc.Level, string, map[string]any) {}

func BenchmarkRouter_fiberv3(b *testing.B) {
	b.Run("middleware_on_sink_noop", func(b *testing.B) {
		app := fiber.New()
		app.Use(Middleware(hc.Config{Sink: discardSink{}, SamplingRate: 1}))
		app.Get("/orders/:id", func(c fiber.Ctx) error {
			hc.Add(c.Context(), "user_id", "u_1")
			return c.SendStatus(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			_, _ = app.Test(req, fiber.TestConfig{Timeout: -1})
		}
	})

	b.Run("normal_logging_no_middleware", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		app := fiber.New()
		app.Get("/orders/:id", func(c fiber.Ctx) error {
			logger.InfoContext(c.Context(), "request_completed",
				slog.String("http.method", c.Method()),
				slog.String("http.path", c.Path()),
				slog.Int("http.status", http.StatusNoContent),
				slog.String("user_id", "u_1"),
			)
			return c.SendStatus(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			_, _ = app.Test(req, fiber.TestConfig{Timeout: -1})
		}
	})
}
