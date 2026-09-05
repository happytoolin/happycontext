package echohappycontext

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/happytoolin/happycontext"
	"github.com/labstack/echo/v4"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	e := echo.New()
	sink := hc.NewTestSink()
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
	if statusField(events[0], "http.status") != http.StatusAccepted {
		t.Fatalf("expected status %d, got %v", http.StatusAccepted, statusField(events[0], "http.status"))
	}
	if fieldValue(events[0], "http.route") != "/orders/:id" {
		t.Fatalf("expected route template, got %v", fieldValue(events[0], "http.route"))
	}
	if fieldValue(events[0], "user_id") != "u_1" {
		t.Fatalf("expected user_id field, got %v", fieldValue(events[0], "user_id"))
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
	sink := hc.NewTestSink()
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
	if events[0].Level() != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level())
	}
	if statusField(events[0], "http.status") != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusInternalServerError)
	}
	if _, ok := fieldValue(events[0], "error").(map[string]any); !ok {
		t.Fatalf("expected structured error field")
	}
}

func TestMiddlewarePanicLogsAndPropagates(t *testing.T) {
	e := echo.New()
	sink := hc.NewTestSink()
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
	if fieldValue(events[0], "http.route") != "/panic/:id" {
		t.Fatalf("route = %v", fieldValue(events[0], "http.route"))
	}
	if statusField(events[0], "http.status") != http.StatusInternalServerError {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusInternalServerError)
	}
	if _, ok := fieldValue(events[0], "panic").(map[string]any); !ok {
		t.Fatalf("expected panic metadata")
	}
}

func TestMiddlewareEchoHTTPErrorKeepsHTTPStatus(t *testing.T) {
	e := echo.New()
	sink := hc.NewTestSink()
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
	if statusField(events[0], "http.status") != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusForbidden)
	}
	if events[0].Level() != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level())
	}
}

func TestMiddlewareCustomMessagePropagates(t *testing.T) {
	e := echo.New()
	sink := hc.NewTestSink()
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
	if events[0].Message() != "done" {
		t.Fatalf("message = %q, want %q", events[0].Message(), "done")
	}
}

func TestMiddlewareLogsStatusFromCustomEchoErrorHandler(t *testing.T) {
	e := echo.New()
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		_ = c.String(http.StatusTeapot, "handled")
	}

	sink := hc.NewTestSink()
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
	if statusField(events[0], "http.status") != http.StatusTeapot {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusTeapot)
	}
	if events[0].Level() != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level())
	}
}

// capturedEvent mirrors the v0 test-facing shape (map fields, int
// numerics) over the v2 TestSink capture, keeping the assertions below
// unchanged from the v0 suite.
// Typed field reads on captured events: fieldValue for any value,
// statusField for the int64 http.status these tests compare against
// int constants.
func fieldValue(ev hc.CapturedEvent, key string) any {
	v, _ := ev.Lookup(key)
	return v
}

func statusField(ev hc.CapturedEvent, key string) int64 {
	v, _ := ev.Lookup(key)
	n, _ := v.(int64)
	return n
}
