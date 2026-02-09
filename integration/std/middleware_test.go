package stdhappycontext

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestMiddlewareDelegatesToCoreAndLogs(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "example", "std-integration")
		w.WriteHeader(http.StatusAccepted)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "done" {
		t.Fatalf("expected message done, got %q", events[0].Message)
	}
	if events[0].Fields["http.status"] != http.StatusAccepted {
		t.Fatalf("expected status %d, got %v", http.StatusAccepted, events[0].Fields["http.status"])
	}
	if events[0].Fields["example"] != "std-integration" {
		t.Fatalf("expected example field, got %v", events[0].Fields["example"])
	}
}

func TestMiddlewarePanicPropagatesAndLogsError(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("bad")
	}))

	rr := httptest.NewRecorder()
	recovered := false
	func() {
		defer func() {
			if recover() != nil {
				recovered = true
			}
		}()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	if !recovered {
		t.Fatal("expected panic to propagate")
	}

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hc.LevelError {
		t.Fatalf("expected error level, got %s", events[0].Level)
	}
	if events[0].Fields["http.status"] != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %v", events[0].Fields["http.status"])
	}
	if _, ok := events[0].Fields["panic"].(map[string]any); !ok {
		t.Fatalf("expected panic field in event")
	}
}

func TestMiddlewareWriteHeaderTwiceLogsFirstCommittedStatus(t *testing.T) {
	backend := &memorySink{}
	mw := Middleware(Config{
		Sink:         backend,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/double-header", nil))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected committed HTTP status %d, got %d", http.StatusCreated, rr.Code)
	}

	events := backend.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusCreated {
		t.Fatalf("expected logged status %d, got %v", http.StatusCreated, events[0].Fields["http.status"])
	}
}

func TestMiddlewarePanicAfterCommittedStatusKeepsCommittedStatus(t *testing.T) {
	backend := &memorySink{}
	mw := Middleware(Config{
		Sink:         backend,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	recovered := false
	func() {
		defer func() {
			if recover() != nil {
				recovered = true
			}
		}()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/panic-after-commit", nil))
	}()

	if !recovered {
		t.Fatal("expected panic to propagate")
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected committed HTTP status %d, got %d", http.StatusCreated, rr.Code)
	}

	events := backend.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != hc.LevelError {
		t.Fatalf("expected error level, got %s", events[0].Level)
	}
	if events[0].Fields["http.status"] != http.StatusCreated {
		t.Fatalf("expected logged status %d, got %v", http.StatusCreated, events[0].Fields["http.status"])
	}
}

func TestMiddlewareSetsRouteFromRequestPattern(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/orders/123", nil)
	mw(mux).ServeHTTP(rr, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	route, ok := events[0].Fields["http.route"].(string)
	if !ok || route == "" {
		t.Fatalf("expected route template, got %#v", events[0].Fields["http.route"])
	}
}

func TestMiddlewarePreservesOptionalInterfaces(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("expected http.Flusher")
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("expected http.Hijacker")
		}
		pusher, ok := w.(http.Pusher)
		if !ok {
			t.Fatalf("expected http.Pusher")
		}
		readerFrom, ok := w.(io.ReaderFrom)
		if !ok {
			t.Fatalf("expected io.ReaderFrom")
		}
		flusher.Flush()
		if _, err := readerFrom.ReadFrom(strings.NewReader("x")); err != nil {
			t.Fatalf("read from failed: %v", err)
		}
		if err := pusher.Push("/asset.js", nil); err != nil {
			t.Fatalf("push failed: %v", err)
		}
		if _, _, err := hijacker.Hijack(); !errors.Is(err, errHijackNotAvailable) {
			t.Fatalf("unexpected hijack error: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	base := &fullOptionalWriter{testOptionalWriter: testOptionalWriter{header: make(http.Header)}}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	h.ServeHTTP(base, req)
	if !base.flushed {
		t.Fatalf("expected flush to be forwarded")
	}
	if !base.pushCalled {
		t.Fatalf("expected push to be forwarded")
	}
	if !base.hijackCalled {
		t.Fatalf("expected hijack to be forwarded")
	}
}

func TestMiddlewareWriteSetsStatusCode(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.Copy(w, bytes.NewBufferString("ok")); err != nil {
			t.Fatalf("copy failed: %v", err)
		}
	}))

	base := &testOptionalWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/copy", nil)
	h.ServeHTTP(base, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusOK {
		t.Fatalf("expected status 200, got %v", events[0].Fields["http.status"])
	}
}

func TestMiddlewareReadFromSetsStatusCode(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 1,
	})

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		readerFrom, ok := w.(io.ReaderFrom)
		if !ok {
			t.Fatalf("expected io.ReaderFrom")
		}
		if _, err := readerFrom.ReadFrom(strings.NewReader("ok")); err != nil {
			t.Fatalf("read from failed: %v", err)
		}
	}))

	base := &fullOptionalWriter{testOptionalWriter: testOptionalWriter{header: make(http.Header)}}
	req := httptest.NewRequest(http.MethodGet, "/copy-readfrom", nil)
	h.ServeHTTP(base, req)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Fields["http.status"] != http.StatusOK {
		t.Fatalf("expected status 200, got %v", events[0].Fields["http.status"])
	}
}

func TestMiddlewareNilSinkStillRunsHandler(t *testing.T) {
	mw := Middleware(Config{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/no-sink", nil))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
}

func TestMiddlewareSamplingDropForHealthyRequest(t *testing.T) {
	sink := &memorySink{}
	mw := Middleware(Config{
		Sink:         sink,
		SamplingRate: 0,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/drop", nil))
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected no events, got %d", got)
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
	for k, v := range fields {
		cp[k] = v
	}
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

type testOptionalWriter struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *testOptionalWriter) Header() http.Header {
	return w.header
}

func (w *testOptionalWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *testOptionalWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

var errHijackNotAvailable = errors.New("hijack unavailable in test writer")

type fullOptionalWriter struct {
	testOptionalWriter
	flushed      bool
	pushCalled   bool
	hijackCalled bool
}

func (w *fullOptionalWriter) Flush() {
	w.flushed = true
}

func (w *fullOptionalWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijackCalled = true
	return nil, nil, errHijackNotAvailable
}

func (w *fullOptionalWriter) Push(_ string, _ *http.PushOptions) error {
	w.pushCalled = true
	return nil
}

func (w *fullOptionalWriter) ReadFrom(src io.Reader) (int64, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return io.Copy(&w.body, src)
}
