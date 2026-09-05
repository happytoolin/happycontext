package stdhappycontext

// Agent E (integration leg) — nil runtime middleware is a documented
// passthrough; the wrapped handler chain must survive nil handlers.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrashNilRuntimePassthrough(t *testing.T) {
	mw := Middleware(nil)
	handler := mw(http.NotFoundHandler())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (passthrough)", rec.Code)
	}
}
