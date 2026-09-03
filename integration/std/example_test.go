package stdhappycontext_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	hc "github.com/happytoolin/happycontext"
	stdhc "github.com/happytoolin/happycontext/integration/std"
)

// ExampleMiddleware shows the two-line adoption: compile once, wrap
// the handler. One canonical event per request.
func ExampleMiddleware() {
	rt := hc.MustCompile(hc.Config{
		Sink:         demoSink{},
		SamplingRate: 1,
	})
	mw := stdhc.Middleware(rt)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hc.Add(r.Context(), "user_id", "u_8472")
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders/123", nil))
	fmt.Println("status:", rec.Code)
	// Output:
	// INFO request_completed http.method=GET http.path=/orders/123 user_id=u_8472 http.status=204 op.domain=http op.name=request duration_ms=0 op.outcome=success
	// status: 204
}

type demoSink struct{}

func (demoSink) Write(_ context.Context, rec *hc.Record) {
	fmt.Printf("%s %s", rec.Level(), rec.Message())
	for _, f := range rec.Fields() {
		if v, ok := rec.Lookup(f.Key()); ok {
			fmt.Printf(" %s=%v", f.Key(), v)
		}
	}
	fmt.Println()
}
