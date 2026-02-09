package fiberv3hlog

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	recovermw "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/happytoolin/hlog"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	app := fiber.New()
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	app.Get("/orders/:id", func(c fiber.Ctx) error {
		hlog.Add(c.Context(), "user_id", "u_1")
		return c.SendStatus(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("fiber v3 test request failed: %v", err)
	}
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected HTTP status %d, got %d", http.StatusNoContent, res.StatusCode)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusNoContent {
		t.Fatalf("expected status %d, got %v", http.StatusNoContent, events[0].Fields["http.status"])
	}
	if events[0].Fields["http.route"] != "/orders/:id" {
		t.Fatalf("expected route template, got %v", events[0].Fields["http.route"])
	}
	if events[0].Fields["user_id"] != "u_1" {
		t.Fatalf("expected user_id field, got %v", events[0].Fields["user_id"])
	}
}

func TestMiddlewareSinkNilStillRunsHandler(t *testing.T) {
	app := fiber.New()
	app.Use(Middleware(hlog.Config{}))
	app.Get("/ok", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusAccepted)
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/ok", nil))
	if err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusAccepted)
	}
}

func TestMiddlewareErrorAndSamplingBehavior(t *testing.T) {
	app := fiber.New()
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 0,
	}))
	app.Get("/drop", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	app.Get("/err", func(c fiber.Ctx) error {
		return errors.New("boom")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/drop", nil)); err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected sampled request to drop, got %d events", got)
	}

	_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/err", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hlog.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
	if events[0].Fields["http.status"] != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusInternalServerError)
	}
	if _, ok := events[0].Fields["error"].(map[string]any); !ok {
		t.Fatalf("expected structured error field")
	}
}

func TestMiddlewarePanicLogsAndPropagates(t *testing.T) {
	app := fiber.New()
	app.Use(recovermw.New())
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	app.Get("/panic/:id", func(c fiber.Ctx) error {
		panic("bad")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/panic/1", nil)); err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.route"] != "/panic/:id" {
		t.Fatalf("route = %v", events[0].Fields["http.route"])
	}
	if events[0].Fields["http.status"] != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusInternalServerError)
	}
	if _, ok := events[0].Fields["panic"].(map[string]any); !ok {
		t.Fatalf("expected panic metadata")
	}
}

func TestMiddlewareFiberErrorKeepsHTTPStatus(t *testing.T) {
	app := fiber.New()
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	app.Get("/too-many", func(c fiber.Ctx) error {
		return fiber.NewError(http.StatusTooManyRequests, "slow down")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/too-many", nil)); err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusTooManyRequests)
	}
	if events[0].Level != hlog.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestMiddlewareCustomMessagePropagates(t *testing.T) {
	app := fiber.New()
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	}))
	app.Get("/ok", func(c fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/ok", nil)); err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "done" {
		t.Fatalf("message = %q, want %q", events[0].Message, "done")
	}
}

func TestMiddlewareLogsStatusFromCustomFiberErrorHandler(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(http.StatusTeapot).SendString("handled")
		},
	})
	sink := &memorySink{}
	app.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	app.Get("/custom-err", func(c fiber.Ctx) error {
		return errors.New("boom")
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/custom-err", nil))
	if err != nil {
		t.Fatalf("fiber v3 request failed: %v", err)
	}
	if res.StatusCode != http.StatusTeapot {
		t.Fatalf("expected HTTP status %d, got %d", http.StatusTeapot, res.StatusCode)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusTeapot {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusTeapot)
	}
	if events[0].Level != hlog.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

type memoryEvent struct {
	Level   string
	Message string
	Fields  map[string]any
}

type memorySink struct {
	mu     sync.Mutex
	events []memoryEvent
}

func (s *memorySink) Write(_ context.Context, level, message string, fields map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]any, len(fields))
	maps.Copy(cp, fields)
	s.events = append(s.events, memoryEvent{
		Level:   level,
		Message: message,
		Fields:  cp,
	})
}

func (s *memorySink) Events() []memoryEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]memoryEvent, len(s.events))
	copy(cp, s.events)
	return cp
}
