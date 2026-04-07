package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/happytoolin/happycontext"
	sloghc "github.com/happytoolin/happycontext/adapter/slog"
	fiberhc "github.com/happytoolin/happycontext/integration/fiber"
)

func TestRouterFiberMiddleware(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sink := sloghc.New(logger)

	app := fiber.New()
	app.Use(fiberhc.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/users/:id", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		id := c.Params("id")

		hc.Add(ctx, "router", "fiber")
		hc.Add(ctx, "event_attached", hc.FromContext(ctx) != nil)
		hc.Add(
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
		hc.SetRoute(ctx, "/users/:id")

		if c.Query("debug") == "1" {
			hc.SetLevel(ctx, hc.LevelDebug)
		}
		if level, ok := hc.GetLevel(ctx); ok {
			hc.Add(ctx, "requested_level", level)
		}
		if c.Query("fail") == "1" {
			hc.Error(ctx, errors.New("demo failure"))
			return c.Status(fiber.StatusInternalServerError).SendString("error")
		}

		return c.SendString("ok")
	})

	t.Run("successful request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123", nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "ok" {
			t.Errorf("expected body 'ok', got %q", string(body))
		}
	})

	t.Run("request with debug", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123?debug=1", nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}
	})

	t.Run("request with failure", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/123?fail=1", nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", resp.StatusCode)
		}
	})
}
