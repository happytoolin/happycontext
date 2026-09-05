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
	"testing"

	"github.com/happytoolin/happycontext"
)

func TestMiddlewareDelegatesToCoreAndLogs(t *testing.T) {
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	}))

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

func TestMiddlewareAppliesCustomMessageFromHandlerContext(t *testing.T) {
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		Message:      "done",
	}))

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hc.SetMessage(r.Context(), "order shipped")
		w.WriteHeader(http.StatusAccepted)
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/orders/123/ship", nil))

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != "order shipped" {
		t.Fatalf("expected message %q, got %q", "order shipped", events[0].Message)
	}
	if events[0].Fields["http.status"] != http.StatusAccepted {
		t.Fatalf("expected status %d, got %v", http.StatusAccepted, events[0].Fields["http.status"])
	}
}

func TestMiddlewarePanicPropagatesAndLogsError(t *testing.T) {
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))

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
	backend := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         backend,
		SamplingRate: 1,
	}))

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
	backend := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         backend,
		SamplingRate: 1,
	}))

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
	var sampledOp string
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
		Sampler: func(in hc.SampleInput) bool {
			sampledOp = in.Operation
			return true
		},
	}))

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
	if name, _ := events[0].Fields["op.name"].(string); name != route {
		t.Fatalf("wire op.name = %q, want route %q", name, route)
	}
	if sampledOp != route {
		t.Fatalf("SampleInput.Operation = %q, want route %q", sampledOp, route)
	}
}

func TestMiddlewarePreservesOptionalInterfaces(t *testing.T) {
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))

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
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))

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
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 1,
	}))

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
	mw := Middleware(hc.MustCompile(hc.Config{}))
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
	sink := newMemorySink()
	mw := Middleware(hc.MustCompile(hc.Config{
		Sink:         sink,
		SamplingRate: 0,
	}))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/drop", nil))
	if got := len(sink.Events()); got != 0 {
		t.Fatalf("expected no events, got %d", got)
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

// TestMiddlewareFlushCommitsStatus pins the implicit-commit rule: the
// first Flush sends the header (200 if unset), so the tracker must
// observe it. A panic after the first flush previously resolved to 500
// against a 200 the client already received.
func TestMiddlewareFlushCommitsStatus(t *testing.T) {
	sink := hc.NewTestSink()
	mw := Middleware(hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1}))

	t.Run("panic after flush keeps the committed 200", func(t *testing.T) {
		sink.Reset()
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			panic("mid-stream")
		}))
		rec := httptest.NewRecorder()
		func() {
			defer func() { _ = recover() }()
			handler.ServeHTTP(rec, httptest.NewRequest("GET", "/s", nil))
		}()
		if rec.Code != http.StatusOK {
			t.Fatalf("client saw %d, want 200", rec.Code)
		}
		st, _ := sink.Events()[0].Lookup("http.status")
		o, _ := sink.Events()[0].Lookup("op.outcome")
		if st != int64(http.StatusOK) || o != string(hc.OutcomePanic) {
			t.Fatalf("log = status:%v outcome:%v, want 200/panic", st, o)
		}
	})

	t.Run("error after flush keeps the committed 200", func(t *testing.T) {
		sink.Reset()
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.(http.Flusher).Flush()
			hc.Error(r.Context(), errors.New("post-flush failure"))
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/s", nil))
		st, _ := sink.Events()[0].Lookup("http.status")
		if st != int64(http.StatusOK) {
			t.Fatalf("status = %v, want committed 200", st)
		}
		// Outcome stays success — outcome is derived from the deferred
		// error pointer, not the recorded error field (v0 semantics) —
		// but the structured error must be present and the event kept.
		if o, _ := sink.Events()[0].Lookup("op.outcome"); o != string(hc.OutcomeSuccess) {
			t.Fatalf("outcome = %v, want success (error field is metadata)", o)
		}
		if _, ok := sink.Events()[0].Lookup("error"); !ok {
			t.Fatal("error field missing")
		}
	})

	t.Run("flush wrappers on all shapes", func(t *testing.T) {
		// httptest.Recorder implements Flusher only; the other promoted
		// shapes are compile-checked by the wrapper types themselves.
		sink.Reset()
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("flusher not promoted")
			}
			f.Flush()
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/s", nil))
		if st, _ := sink.Events()[0].Lookup("http.status"); st != int64(http.StatusOK) {
			t.Fatalf("status = %v, want 200 after plain flush", st)
		}
	})
}
