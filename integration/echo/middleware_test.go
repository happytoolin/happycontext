package echohappycontext

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/happytoolin/happycontext"
	"github.com/labstack/echo/v4"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	e := echo.New()
	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	e.GET("/orders/:id", func(c echo.Context) error {
		happycontext.Add(c.Request().Context(), "user_id", "u_1")
		return c.NoContent(http.StatusAccepted)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusAccepted {
		t.Fatalf("expected status %d, got %v", http.StatusAccepted, events[0].Fields["http.status"])
	}
	if events[0].Fields["http.route"] != "/orders/:id" {
		t.Fatalf("expected route template, got %v", events[0].Fields["http.route"])
	}
	if events[0].Fields["user_id"] != "u_1" {
		t.Fatalf("expected user_id field, got %v", events[0].Fields["user_id"])
	}
}

func TestMiddlewareSinkNilStillRunsHandler(t *testing.T) {
	e := echo.New()
	e.Use(Middleware(happycontext.Config{}))
	e.GET("/ok", func(c echo.Context) error {
		return c.NoContent(http.StatusAccepted)
	})

	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
}

func TestMiddlewareErrorAndSamplingBehavior(t *testing.T) {
	e := echo.New()
	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 0,
	}))
	e.GET("/drop", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/err", func(c echo.Context) error {
		return errors.New("boom")
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/drop", nil))
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected sampled request to drop, got %d events", got)
	}

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/err", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != happycontext.LevelError {
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
	e := echo.New()
	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	e.GET("/panic/:id", func(c echo.Context) error {
		panic("bad")
	})

	recovered := false
	func() {
		defer func() {
			if recover() != nil {
				recovered = true
			}
		}()
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic/1", nil))
	}()
	if !recovered {
		t.Fatal("expected panic propagation")
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

func TestMiddlewareEchoHTTPErrorKeepsHTTPStatus(t *testing.T) {
	e := echo.New()
	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	e.GET("/forbidden", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusForbidden, "nope")
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/forbidden", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusForbidden)
	}
	if events[0].Level != happycontext.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestMiddlewareCustomMessagePropagates(t *testing.T) {
	e := echo.New()
	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	}))
	e.GET("/ok", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "done" {
		t.Fatalf("message = %q, want %q", events[0].Message, "done")
	}
}

func TestMiddlewareLogsStatusFromCustomEchoErrorHandler(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		_ = c.String(http.StatusTeapot, "handled")
	}

	sink := &memorySink{}
	e.Use(Middleware(happycontext.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	e.GET("/custom-err", func(c echo.Context) error {
		return errors.New("boom")
	})

	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/custom-err", nil))
	if rr.Code != http.StatusTeapot {
		t.Fatalf("expected status %d, got %d", http.StatusTeapot, rr.Code)
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusTeapot {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusTeapot)
	}
	if events[0].Level != happycontext.LevelError {
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
