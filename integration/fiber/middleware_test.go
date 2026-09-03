package fiberhappycontext

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	recovermw "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/happytoolin/happycontext"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	app := fiber.New()
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	app.Get("/orders/:id", func(c *fiber.Ctx) error {
		hc.Add(c.UserContext(), "user_id", "u_1")
		return c.SendStatus(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	res, err := app.Test(req)
	if err != nil {
		t.Fatalf("fiber test request failed: %v", err)
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
	app.Use(Middleware(hc.MustCompile(hc.Config{})))
	app.Get("/ok", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusAccepted)
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/ok", nil))
	if err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusAccepted)
	}
}

func TestMiddlewareErrorAndSamplingBehavior(t *testing.T) {
	app := fiber.New()
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 0,
	})))
	app.Get("/drop", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})
	app.Get("/err", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/drop", nil)); err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected sampled request to drop, got %d events", got)
	}

	_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/err", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hc.LevelError {
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
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	app.Get("/panic/:id", func(c *fiber.Ctx) error {
		panic("bad")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/panic/1", nil)); err != nil {
		t.Fatalf("fiber request failed: %v", err)
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
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	app.Get("/too-many", func(c *fiber.Ctx) error {
		return fiber.NewError(http.StatusTooManyRequests, "slow down")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/too-many", nil)); err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusTooManyRequests)
	}
	if events[0].Level != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestMiddlewareCustomMessagePropagates(t *testing.T) {
	app := fiber.New()
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	})))
	app.Get("/ok", func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/ok", nil)); err != nil {
		t.Fatalf("fiber request failed: %v", err)
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
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(http.StatusTeapot).SendString("handled")
		},
	})
	sink := newMemorySink()
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	app.Get("/custom-err", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/custom-err", nil))
	if err != nil {
		t.Fatalf("fiber request failed: %v", err)
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
	if events[0].Level != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestMiddlewareReturnsCustomFiberErrorHandlerFailure(t *testing.T) {
	handlerErr := errors.New("handler failed")
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return handlerErr
		},
	})
	sink := newMemorySink()
	var upstreamErr error
	app.Use(func(c *fiber.Ctx) error {
		upstreamErr = c.Next()
		return upstreamErr
	})
	app.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	app.Get("/custom-err-failure", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})

	if _, err := app.Test(httptest.NewRequest(http.MethodGet, "/custom-err-failure", nil)); err != nil {
		t.Fatalf("fiber request failed: %v", err)
	}
	if !errors.Is(upstreamErr, handlerErr) {
		t.Fatalf("upstream error = %v, want %v", upstreamErr, handlerErr)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	errField, ok := events[0].Fields["error"].(map[string]any)
	if !ok {
		t.Fatal("expected structured error field")
	}
	if errField["message"] != "handler failed" {
		t.Fatalf("error message = %v, want handler failed", errField["message"])
	}
}

// capturedEvent mirrors the v0 test-facing shape (map fields, int
// numerics) over the v2 TestSink capture, keeping the assertions below
// unchanged from the v0 suite.
type capturedEvent struct {
	Level   hc.Level
	Message string
	Fields  map[string]any
}

type memorySink struct {
	ts *hc.TestSink
}

func newMemorySink() *memorySink {
	return &memorySink{ts: hc.NewTestSink()}
}

func (s *memorySink) Write(ctx context.Context, rec *hc.Record) {
	s.ts.Write(ctx, rec)
}

func (s *memorySink) Events() []capturedEvent {
	captured := s.ts.Events()
	out := make([]capturedEvent, 0, len(captured))
	for _, ev := range captured {
		fields := make(map[string]any, len(ev.Fields())+4)
		for _, f := range ev.Fields() {
			v, _ := ev.Lookup(f.Key())
			if i, ok := v.(int64); ok {
				fields[f.Key()] = int(i)
			} else {
				fields[f.Key()] = v
			}
		}
		out = append(out, capturedEvent{Level: ev.Level(), Message: ev.Message(), Fields: fields})
	}
	return out
}
