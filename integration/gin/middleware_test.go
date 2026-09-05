package ginhappycontext

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/happycontext"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	r.GET("/orders/:id", func(c *gin.Context) {
		hc.Add(c.Request.Context(), "user_id", "u_1")
		c.Status(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if statusField(events[0], "http.status") != http.StatusCreated {
		t.Fatalf("expected status %d, got %v", http.StatusCreated, statusField(events[0], "http.status"))
	}
	if fieldValue(events[0], "http.route") != "/orders/:id" {
		t.Fatalf("expected route template, got %v", fieldValue(events[0], "http.route"))
	}
	if fieldValue(events[0], "user_id") != "u_1" {
		t.Fatalf("expected user_id field, got %v", fieldValue(events[0], "user_id"))
	}
}

func TestMiddlewareSinkNilStillRunsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{})))
	r.GET("/ok", func(c *gin.Context) {
		c.Status(http.StatusAccepted)
	})

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
}

func TestMiddlewareErrorAndSamplingBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 0,
	})))
	r.GET("/drop", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.GET("/err", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
		c.Status(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/drop", nil))
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected sampled request to drop, got %d events", got)
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/err", nil))
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
	gin.SetMode(gin.TestMode)
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	r.GET("/panic/:id", func(c *gin.Context) {
		panic("bad")
	})

	recovered := false
	func() {
		defer func() {
			if recover() != nil {
				recovered = true
			}
		}()
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic/1", nil))
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

func TestMiddlewareLogsNoRouteWithoutTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	r.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].Lookup("http.route"); ok {
		t.Fatalf("did not expect route template for unmatched route")
	}
	if statusField(events[0], "http.status") != http.StatusNotFound {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusNotFound)
	}
}

func TestMiddlewareGinErrorKeepsCommittedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	r.GET("/too-many", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
		c.AbortWithStatus(http.StatusTooManyRequests)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/too-many", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if statusField(events[0], "http.status") != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want %d", statusField(events[0], "http.status"), http.StatusTooManyRequests)
	}
	if events[0].Level() != hc.LevelError {
		t.Fatalf("level = %s, want ERROR", events[0].Level())
	}
}

func TestMiddlewareGinErrorUsesUnderlyingErrorMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := hc.NewTestSink()
	r := gin.New()
	r.Use(Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	})))
	r.GET("/err", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
		c.AbortWithStatus(http.StatusInternalServerError)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/err", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	errField, ok := fieldValue(events[0], "error").(map[string]any)
	if !ok {
		t.Fatalf("expected structured error field")
	}
	if errField["message"] != "boom" {
		t.Fatalf("message = %v, want boom", errField["message"])
	}
	if errField["type"] != "*errors.errorString" {
		t.Fatalf("type = %v, want *errors.errorString", errField["type"])
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
