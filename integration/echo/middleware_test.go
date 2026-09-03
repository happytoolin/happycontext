package echohappycontext

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/happycontext"
	"github.com/labstack/echo/v4"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	e := echo.New()
	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	e.GET("/orders/:id", func(c echo.Context) error {
		hc.Add(c.Request().Context(), "user_id", "u_1")
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
	e.Use(Middleware(hc.MustCompile(hc.Config{})))
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
	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 0,
	})))
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
	e := echo.New()
	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
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
	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
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
	if events[0].Level != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
	}
}

func TestMiddlewareCustomMessagePropagates(t *testing.T) {
	e := echo.New()
	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	})))
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

	sink := newMemorySink()
	e.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
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
	if events[0].Level != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level)
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
