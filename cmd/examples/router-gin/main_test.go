package main

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	ginhappycontext "github.com/happytoolin/happycontext/integration/gin"
)

func TestRouterGinMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := slogadapter.New(logger)

	r := gin.New()
	r.Use(ginhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	r.GET("/users/:id", func(c *gin.Context) {
		ctx := c.Request.Context()
		id := c.Param("id")

		hc.Add(ctx, "router", "gin")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.Add(
			ctx,
			"user", map[string]any{
				"id":   id,
				"plan": "pro",
			},
			"request", map[string]any{
				"feature": "profile",
				"tags":    []string{"examples", "router-gin"},
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
			c.Status(500)
			return
		}

		c.Status(200)
	})

	t.Run("successful request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("request with debug", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123?debug=1", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("request with failure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123?fail=1", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}
	})
}
