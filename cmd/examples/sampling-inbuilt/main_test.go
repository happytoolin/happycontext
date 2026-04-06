package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/happytoolin/happycontext"
	slogadapter "github.com/happytoolin/happycontext/adapter/slog"
	stdhappycontext "github.com/happytoolin/happycontext/integration/std"
)

func TestSamplingInbuiltMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	sink := slogadapter.New(logger)

	mw := stdhappycontext.Middleware(hc.Config{
		Sink: sink,
		Sampler: hc.ChainSampler(
			hc.RateSampler(0.05),
			hc.KeepErrors(),
			hc.KeepPathPrefix("/users/vip"),
			hc.KeepSlowerThan(250*time.Millisecond),
		),
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleUser(w, r, "standard")
	})
	mux.HandleFunc("/users/vip/{id}", func(w http.ResponseWriter, r *http.Request) {
		handleUser(w, r, "vip")
	})

	handler := mw(mux)

	t.Run("standard user request", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/123", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}
	})

	t.Run("vip user request with path prefix match", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/vip/456", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		output := buf.String()
		if !strings.Contains(output, "vip") {
			t.Error("expected log output to contain 'vip'")
		}
	})

	t.Run("slow request triggers sampling", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/789?slow=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rec.Code)
		}

		// Slow requests (350ms) should be logged due to KeepSlowerThan(250ms)
		output := buf.String()
		if output == "" {
			t.Error("expected slow request to be logged")
		}
	})

	t.Run("error request is always sampled", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest("GET", "/users/999?fail=1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", rec.Code)
		}

		// Error requests should always be logged due to KeepErrors()
		output := buf.String()
		if !strings.Contains(output, "error") && !strings.Contains(output, "failure") {
			t.Error("expected error request to be logged")
		}
	})
}
