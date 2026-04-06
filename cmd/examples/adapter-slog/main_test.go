package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
)

func TestAdapterSlogMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	sink := slogadapter.New(logger)
	mw := stdhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1, Message: "request handled"})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := r.PathValue("id")

		hc.Add(ctx, "example", "adapter-slog")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.Add(
			ctx,
			"user", map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request", map[string]any{
				"feature": "checkout",
				"tags":    []string{"examples", "slog"},
			},
		)
		hc.SetRoute(ctx, "/users/{id}")

		if r.URL.Query().Get("debug") == "1" {
			hc.SetLevel(ctx, hc.LevelDebug)
		}
		if level, ok := hc.GetLevel(ctx); ok {
			hc.Add(ctx, "requested_level", level)
		}

		if r.URL.Query().Get("fail") == "1" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := mw(mux)

	t.Run("successful request logs event", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/456", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		output := buf.String()
		if !strings.Contains(output, "request handled") {
			t.Error("expected log output to contain 'request handled'")
		}
		if !strings.Contains(output, "adapter-slog") {
			t.Error("expected log output to contain 'adapter-slog'")
		}
	})

	t.Run("request with debug flag", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/789?debug=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("request with failure", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/999?fail=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}
	})
}
