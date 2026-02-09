package echohappycontext

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/happycontext"
	"github.com/labstack/echo/v4"
)

type discardSink struct{}

func (discardSink) Write(hc.Level, string, map[string]any) {}

func BenchmarkRouter_echo(b *testing.B) {
	b.Run("middleware_on_sink_noop", func(b *testing.B) {
		e := echo.New()
		e.Use(Middleware(hc.Config{Sink: discardSink{}, SamplingRate: 1}))
		e.GET("/orders/:id", func(c echo.Context) error {
			hc.Add(c.Request().Context(), "user_id", "u_1")
			return c.NoContent(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			e.ServeHTTP(rr, req)
		}
	})

	b.Run("normal_logging_no_middleware", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		e := echo.New()
		e.GET("/orders/:id", func(c echo.Context) error {
			logger.InfoContext(c.Request().Context(), "request_completed",
				slog.String("http.method", c.Request().Method),
				slog.String("http.path", c.Request().URL.Path),
				slog.Int("http.status", http.StatusNoContent),
				slog.String("user_id", "u_1"),
			)
			return c.NoContent(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			e.ServeHTTP(rr, req)
		}
	})
}
