package ginhlog

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/happytoolin/hlog"
)

func TestMiddlewareCapturesRouteAndFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sink := &memorySink{}
	r := gin.New()
	r.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	r.GET("/orders/:id", func(c *gin.Context) {
		hlog.Add(c.Request.Context(), "user_id", "u_1")
		c.Status(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusCreated {
		t.Fatalf("expected status %d, got %v", http.StatusCreated, events[0].Fields["http.status"])
	}
	if events[0].Fields["http.route"] != "/orders/:id" {
		t.Fatalf("expected route template, got %v", events[0].Fields["http.route"])
	}
	if events[0].Fields["user_id"] != "u_1" {
		t.Fatalf("expected user_id field, got %v", events[0].Fields["user_id"])
	}
}

func TestMiddlewareSinkNilStillRunsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(hlog.Config{}))
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
	sink := &memorySink{}
	r := gin.New()
	r.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 0,
	}))
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
	gin.SetMode(gin.TestMode)
	sink := &memorySink{}
	r := gin.New()
	r.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
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

func TestMiddlewareLogsNoRouteWithoutTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &memorySink{}
	r := gin.New()
	r.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	r.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if _, ok := events[0].Fields["http.route"]; ok {
		t.Fatalf("did not expect route template for unmatched route")
	}
	if events[0].Fields["http.status"] != http.StatusNotFound {
		t.Fatalf("status = %v, want %d", events[0].Fields["http.status"], http.StatusNotFound)
	}
}

func TestMiddlewareGinErrorKeepsCommittedStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &memorySink{}
	r := gin.New()
	r.Use(Middleware(hlog.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))
	r.GET("/too-many", func(c *gin.Context) {
		_ = c.Error(errors.New("boom"))
		c.AbortWithStatus(http.StatusTooManyRequests)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/too-many", nil))
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
