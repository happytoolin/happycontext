package ginhappycontext

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
)

type discardSink struct{}

func (discardSink) Write(hc.Level, string, map[string]any) {}

func BenchmarkRouter_gin(b *testing.B) {
	gin.SetMode(gin.TestMode)

	b.Run("middleware_on_sink_noop", func(b *testing.B) {
		r := gin.New()
		r.Use(Middleware(hc.Config{Sink: discardSink{}, SamplingRate: 1}))
		r.GET("/orders/:id", func(c *gin.Context) {
			hc.Add(c.Request.Context(), "user_id", "u_1")
			c.Status(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
		}
	})

	b.Run("normal_logging_no_middleware", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		r := gin.New()
		r.GET("/orders/:id", func(c *gin.Context) {
			logger.InfoContext(c.Request.Context(), "request_completed",
				slog.String("http.method", c.Request.Method),
				slog.String("http.path", c.Request.URL.Path),
				slog.Int("http.status", http.StatusNoContent),
				slog.String("user_id", "u_1"),
			)
			c.Status(http.StatusNoContent)
		})
		req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
		}
	})
}
