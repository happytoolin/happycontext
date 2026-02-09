package stdhappycontext

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/happycontext"
)

type discardSink struct{}

func (discardSink) Write(context.Context, string, string, map[string]any) {}

func BenchmarkRouter_std(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	handlerHappycontextAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		happycontext.Add(r.Context(), "user_id", "u_1")
		w.WriteHeader(http.StatusNoContent)
	})

	b.Run("middleware_on_sink_noop", func(b *testing.B) {
		mw := Middleware(Config{Sink: discardSink{}, SamplingRate: 1})
		wrapped := mw(handlerHappycontextAPI)

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)
		}
	})

	b.Run("normal_logging_no_middleware", func(b *testing.B) {
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger.InfoContext(r.Context(), "request_completed",
				slog.String("http.method", r.Method),
				slog.String("http.path", r.URL.Path),
				slog.Int("http.status", http.StatusNoContent),
				slog.String("user_id", "u_1"),
			)
			w.WriteHeader(http.StatusNoContent)
		})

		b.ReportAllocs()
		for b.Loop() {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}
	})
}
