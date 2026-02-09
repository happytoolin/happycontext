package examples

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	recoverv2 "github.com/gofiber/fiber/v2/middleware/recover"
	fiberv3 "github.com/gofiber/fiber/v3"
	recoverv3 "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/happytoolin/happycontext"
	echohappycontext "github.com/happytoolin/happycontext/integration/echo"
	fiberhappycontext "github.com/happytoolin/happycontext/integration/fiber"
	fiberv3happycontext "github.com/happytoolin/happycontext/integration/fiberv3"
	ginhappycontext "github.com/happytoolin/happycontext/integration/gin"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
	"github.com/labstack/echo/v4"
)

type runResult struct {
	event         hc.CapturedEvent
	panicObserved bool
}

type comparableResult struct {
	level    string
	status   int
	message  string
	method   string
	path     string
	hasError bool
	hasPanic bool
}

func TestIntegrationConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type runner struct {
		name string
		run  func(t *testing.T, mode string) runResult
	}
	runners := []runner{
		{name: "std", run: runStd},
		{name: "gin", run: runGin},
		{name: "echo", run: runEcho},
		{name: "fiber", run: runFiber},
		{name: "fiberv3", run: runFiberV3},
	}

	modes := []string{"success", "error", "panic"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			results := make(map[string]runResult, len(runners))
			for _, r := range runners {
				results[r.name] = r.run(t, mode)
			}

			var baseline comparableResult
			for i, r := range runners {
				out := results[r.name]
				assertConsistency(t, mode, out)
				got := normalizeResult(t, out)
				if i == 0 {
					baseline = got
					continue
				}
				if got != baseline {
					t.Fatalf("%s result mismatch with baseline: got=%+v want=%+v", r.name, got, baseline)
				}
			}
		})
	}
}

func assertConsistency(t *testing.T, mode string, out runResult) {
	t.Helper()

	if out.event.Fields["http.status"] == nil {
		t.Fatalf("expected http.status field")
	}
	route, _ := out.event.Fields["http.route"].(string)
	if route == "" || !strings.Contains(route, "/orders") {
		t.Fatalf("unexpected route field: %v", out.event.Fields["http.route"])
	}

	switch mode {
	case "success":
		if out.event.Level != hc.LevelInfo {
			t.Fatalf("level = %s, want INFO", out.event.Level)
		}
		if statusFromField(t, out.event.Fields["http.status"]) != http.StatusOK {
			t.Fatalf("status = %v, want %d", out.event.Fields["http.status"], http.StatusOK)
		}
	case "error":
		if out.event.Level != hc.LevelError {
			t.Fatalf("level = %s, want ERROR", out.event.Level)
		}
		if statusFromField(t, out.event.Fields["http.status"]) != http.StatusInternalServerError {
			t.Fatalf("status = %v, want %d", out.event.Fields["http.status"], http.StatusInternalServerError)
		}
		if _, ok := out.event.Fields["error"].(map[string]any); !ok {
			t.Fatalf("expected structured error field")
		}
	case "panic":
		if !out.panicObserved {
			t.Fatal("expected panic propagation/observation")
		}
		if out.event.Level != hc.LevelError {
			t.Fatalf("level = %s, want ERROR", out.event.Level)
		}
		if statusFromField(t, out.event.Fields["http.status"]) != http.StatusInternalServerError {
			t.Fatalf("status = %v, want %d", out.event.Fields["http.status"], http.StatusInternalServerError)
		}
		if _, ok := out.event.Fields["panic"].(map[string]any); !ok {
			t.Fatalf("expected panic field")
		}
	}
}

func TestIntegrationImplicitErrorStatusConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type runner struct {
		name string
		run  func(t *testing.T) runResult
	}
	runners := []runner{
		{name: "gin", run: runGinImplicitError},
		{name: "echo", run: runEchoImplicitError},
		{name: "fiber", run: runFiberImplicitError},
		{name: "fiberv3", run: runFiberV3ImplicitError},
	}

	for _, r := range runners {
		t.Run(r.name, func(t *testing.T) {
			out := r.run(t)
			status := statusFromField(t, out.event.Fields["http.status"])
			if status != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", status, http.StatusInternalServerError)
			}
			if out.event.Level != hc.LevelError {
				t.Fatalf("level = %s, want ERROR", out.event.Level)
			}
			if _, ok := out.event.Fields["error"].(map[string]any); !ok {
				t.Fatalf("expected structured error field")
			}
		})
	}
}

func runStd(t *testing.T, mode string) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	mw := stdhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "error":
			hc.Error(r.Context(), errors.New("boom"))
			w.WriteHeader(http.StatusInternalServerError)
			return
		case "panic":
			panic("boom")
		}
		w.WriteHeader(http.StatusOK)
	})
	var panicObserved bool
	func() {
		defer func() {
			if recover() != nil {
				panicObserved = true
			}
		}()
		mw(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	}()
	return runResult{event: onlyEvent(t, sink), panicObserved: panicObserved}
}

func runGin(t *testing.T, mode string) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(ginhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	r.GET("/orders/:id", func(c *gin.Context) {
		switch mode {
		case "error":
			_ = c.Error(errors.New("boom"))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		case "panic":
			panic("boom")
		}
		c.Status(http.StatusOK)
	})
	var panicObserved bool
	func() {
		defer func() {
			if recover() != nil {
				panicObserved = true
			}
		}()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	}()
	return runResult{event: onlyEvent(t, sink), panicObserved: panicObserved}
}

func runEcho(t *testing.T, mode string) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	e := echo.New()
	e.Use(echohappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	e.GET("/orders/:id", func(c echo.Context) error {
		switch mode {
		case "error":
			return echo.NewHTTPError(http.StatusInternalServerError, "boom")
		case "panic":
			panic("boom")
		}
		return c.NoContent(http.StatusOK)
	})
	var panicObserved bool
	func() {
		defer func() {
			if recover() != nil {
				panicObserved = true
			}
		}()
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	}()
	return runResult{event: onlyEvent(t, sink), panicObserved: panicObserved}
}

func runFiber(t *testing.T, mode string) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	app := fiber.New()
	app.Use(recoverv2.New())
	app.Use(fiberhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/orders/:id", func(c *fiber.Ctx) error {
		switch mode {
		case "error":
			return fiber.NewError(http.StatusInternalServerError, "boom")
		case "panic":
			panic("boom")
		}
		return c.SendStatus(http.StatusOK)
	})
	_, err := app.Test(httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	event := onlyEvent(t, sink)
	_, hasPanic := event.Fields["panic"].(map[string]any)
	return runResult{event: event, panicObserved: err != nil || hasPanic}
}

func runFiberV3(t *testing.T, mode string) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	app := fiberv3.New()
	app.Use(recoverv3.New())
	app.Use(fiberv3happycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/orders/:id", func(c fiberv3.Ctx) error {
		switch mode {
		case "error":
			return fiber.NewError(http.StatusInternalServerError, "boom")
		case "panic":
			panic("boom")
		}
		return c.SendStatus(http.StatusOK)
	})
	_, err := app.Test(httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	event := onlyEvent(t, sink)
	_, hasPanic := event.Fields["panic"].(map[string]any)
	return runResult{event: event, panicObserved: err != nil || hasPanic}
}

func runGinImplicitError(t *testing.T) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(ginhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	r.GET("/orders/:id", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	return runResult{event: onlyEvent(t, sink)}
}

func runEchoImplicitError(t *testing.T) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	e := echo.New()
	e.Use(echohappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	e.GET("/orders/:id", func(c echo.Context) error {
		return errors.New("boom")
	})
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	return runResult{event: onlyEvent(t, sink)}
}

func runFiberImplicitError(t *testing.T) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	app := fiber.New()
	app.Use(fiberhappycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/orders/:id", func(c *fiber.Ctx) error {
		return errors.New("boom")
	})
	_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	return runResult{event: onlyEvent(t, sink)}
}

func runFiberV3ImplicitError(t *testing.T) runResult {
	t.Helper()
	sink := hc.NewTestSink()
	app := fiberv3.New()
	app.Use(fiberv3happycontext.Middleware(hc.Config{Sink: sink, SamplingRate: 1}))
	app.Get("/orders/:id", func(c fiberv3.Ctx) error {
		return errors.New("boom")
	})
	_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/orders/1", nil))
	return runResult{event: onlyEvent(t, sink)}
}

func onlyEvent(t *testing.T, sink *hc.TestSink) hc.CapturedEvent {
	t.Helper()
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	return events[0]
}

func normalizeResult(t *testing.T, out runResult) comparableResult {
	t.Helper()

	_, hasError := out.event.Fields["error"].(map[string]any)
	_, hasPanic := out.event.Fields["panic"].(map[string]any)
	method, _ := out.event.Fields["http.method"].(string)
	path, _ := out.event.Fields["http.path"].(string)
	return comparableResult{
		level:    out.event.Level,
		status:   statusFromField(t, out.event.Fields["http.status"]),
		message:  out.event.Message,
		method:   method,
		path:     path,
		hasError: hasError,
		hasPanic: hasPanic,
	}
}

func statusFromField(t *testing.T, value any) int {
	t.Helper()

	status, ok := value.(int)
	if !ok {
		t.Fatalf("expected int status, got %T (%v)", value, value)
	}
	return status
}
