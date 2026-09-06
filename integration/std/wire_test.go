package stdhappycontext

// Wire-level reality tests: no test doubles at all — the sink is the
// first-party JSONSink emitting the canonical line, the traffic is
// real HTTP against the real middleware, and the oracle parses the
// actual emitted lines. (The three logger bridges' equivalent runs in
// cmd/examples, where those modules are already dependencies.)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

// mustWireLine parses one emitted JSON line and asserts the canonical
// envelope every consumer relies on.
func parseWireLine(t *testing.T, ln string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(ln), &m); err != nil {
		t.Fatalf("wire line unparseable: %v: %s", err, ln)
	}
	for _, k := range []string{"time", "level", "message", "http.method", "http.path", "http.route", "op.name", "http.status", "op.domain", "duration_ms", "op.outcome"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("canonical key %q missing: %s", k, ln)
		}
	}
	outcome, ocOK := m["op.outcome"].(string)
	statusF, stOK := m["http.status"].(float64)
	if !ocOK || !stOK {
		t.Fatalf("canonical envelope mistyped: %s", ln)
	}
	status := int(statusF)
	switch outcome {
	case "panic":
		if _, ok := m["panic"].(map[string]any); !ok {
			t.Fatalf("panic outcome without structured panic field: %s", ln)
		}
	case "failure":
		if _, ok := m["error"].(map[string]any); !ok {
			t.Fatalf("failure outcome without structured error field: %s", ln)
		}
		if status < 500 {
			t.Fatalf("failure outcome with status %d: %s", status, ln)
		}
	case "success":
		if _, ok := m["error"]; ok {
			t.Fatalf("success outcome carries an error field: %s", ln)
		}
	default:
		t.Fatalf("unexpected outcome %q: %s", outcome, ln)
	}
	return m
}

// TestWireMixedTrafficRealLogger drives concurrent mixed traffic —
// success, error, panic, streaming-flush, kitchen-sink — through the
// real middleware and the first-party JSONSink (one shared sink, the
// production shape; the slog/zap/zerolog equivalents live in
// cmd/examples); every emitted line must parse, carry the full
// canonical envelope, and be internally coherent.
func TestWireMixedTrafficRealLogger(t *testing.T) {
	var buf bytes.Buffer
	// One JSONSink, mutex-serialized by the sink itself: the shared
	// bytes.Buffer is safe exactly as it would be in production.
	rt := hc.MustCompile(hc.Config{Sink: hc.NewJSONSink(&buf), SamplingRate: 1})
	mw := Middleware(rt)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "id", r.PathValue("id"))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /err/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "id", r.PathValue("id"))
		hc.Error(r.Context(), fmt.Errorf("wire failure %s", r.PathValue("id")))
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /panic/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "id", r.PathValue("id"))
		panic("wire panic " + r.PathValue("id"))
	})
	mux.HandleFunc("GET /stream/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "id", r.PathValue("id"))
		f := w.(http.Flusher)
		for i := range 3 {
			fmt.Fprintf(w, "chunk %d\n", i)
			f.Flush()
		}
	})
	mux.HandleFunc("GET /kitchen/{id}", func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(),
			"id", r.PathValue("id"),
			"utf8", "\xff\xfe garbage",
			"deep", map[string]any{"a": []any{1, "two", nil}},
			"big", strings.Repeat("x", 4096),
		)
		w.WriteHeader(http.StatusTeapot)
	})

	srv := httptest.NewServer(mw(mux))
	defer srv.Close()

	// DisableKeepAlives: a fresh connection per request means the client
	// never retries after the panic route closes a reused connection —
	// one server handling (one event) per request, deterministic without
	// leaving real HTTP behind. srv.Client() is shared: configure once.
	client := srv.Client()
	client.Transport = &http.Transport{DisableKeepAlives: true}

	const workers = 8
	const per = 15
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range per {
				id := fmt.Sprintf("w%d-%d", w, i)
				for _, p := range []string{"/ok/", "/err/", "/panic/", "/stream/", "/kitchen/"} {
					resp, err := client.Get(srv.URL + p + id)
					if err == nil {
						_ = resp.Body.Close()
					}
				}
			}
		})
	}
	wg.Wait()
	// Client returns precede the server's deferred emissions; Close
	// waits for outstanding handlers, so the buffer is quiescent.
	srv.Close()

	lines := nonEmptyLines(buf.String())
	if got, want := len(lines), workers*per*5; got != want {
		t.Fatalf("emitted %d lines, want %d", got, want)
	}
	outcomes := map[string]int{}
	for _, ln := range lines {
		m := parseWireLine(t, ln)
		outcomes[m["op.outcome"].(string)]++
	}
	// Exact mix: every /err is failure, every /panic is panic, the
	// rest success (4xx teapot is success-with-status, not failure).
	if outcomes["failure"] != workers*per || outcomes["panic"] != workers*per || outcomes["success"] != workers*per*3 {
		t.Fatalf("outcome mix = %v", outcomes)
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for ln := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

// TestWireRouteAndOperationShareTheTemplate pins, on the real wire,
// that the sampler-visible operation name and the emitted op.name are
// the same route template.
func TestWireRouteAndOperationShareTheTemplate(t *testing.T) {
	var sampled string
	var buf bytes.Buffer
	rt := hc.MustCompile(hc.Config{
		Sink:         hc.NewJSONSink(&buf),
		SamplingRate: 1,
		Sampler: func(in hc.SampleInput) bool {
			sampled = in.Operation
			return true
		},
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(Middleware(rt)(mux))

	resp, err := srv.Client().Get(srv.URL + "/orders/o_42")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	// Quiesce before reading shared state: client returns precede the
	// server's deferred emission, and this test reads buf and sampled
	// directly (the buffering that makes this safe today is a net/http
	// implementation detail — Close makes it explicit).
	srv.Close()

	lines := nonEmptyLines(buf.String())
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	m := parseWireLine(t, lines[0])
	// req.Pattern carries the method for method-matched patterns; the
	// invariant is that all three views agree on the same template.
	const want = "GET /orders/{id}"
	if sampled != want || m["op.name"] != want || m["http.route"] != want {
		t.Fatalf("sampler=%q op.name=%v route=%v, want all %q", sampled, m["op.name"], m["http.route"], want)
	}
}
