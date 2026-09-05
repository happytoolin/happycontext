package stdhappycontext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

// TestMiddlewareConcurrentStatusIntegrity pins the tracker-pool race
// fix: under concurrent requests, every event must carry its own
// request's status (a released-then-reset tracker would log 0→200).
func TestMiddlewareConcurrentStatusIntegrity(t *testing.T) {
	sink := hc.NewTestSink()
	rt := hc.MustCompile(hc.Config{Sink: sink, SamplingRate: 1})
	mw := Middleware(rt)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		switch code {
		case "201":
			w.WriteHeader(http.StatusCreated)
		case "404":
			w.WriteHeader(http.StatusNotFound)
		case "500":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))

	want := map[string]int{
		"201": http.StatusCreated,
		"404": http.StatusNotFound,
		"500": http.StatusInternalServerError,
		"":    http.StatusOK,
	}

	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 100 {
				code := []string{"201", "404", "500", ""}[(g+i)%4]
				req := httptest.NewRequest(http.MethodGet, "/x?code="+code, nil).WithContext(context.Background())
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				if got := want[code]; rr.Code != got {
					t.Errorf("code %q: response = %d, want %d", code, rr.Code, got)
					return
				}
			}
		})
	}
	wg.Wait()

	events := sink.Events()
	if len(events) != 800 {
		t.Fatalf("events = %d, want 800", len(events))
	}
	for _, ev := range events {
		status, ok := ev.Lookup("http.status")
		if !ok {
			t.Fatal("missing http.status")
		}
		switch status {
		case int64(http.StatusCreated), int64(http.StatusNotFound), int64(http.StatusInternalServerError), int64(http.StatusOK):
		default:
			t.Fatalf("corrupted status under concurrency: %v", status)
		}
	}
}
