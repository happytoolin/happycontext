package common

import (
	"context"
	"testing"

	hc "github.com/happytoolin/happycontext"
)

func TestMethodAnyProducesPlainStrings(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PROPFIND", ""} {
		got := methodAny(method)
		if _, ok := got.(string); !ok {
			t.Fatalf("methodAny(%q) dynamic type = %T, want string", method, got)
		}
		if got != any(method) {
			t.Fatalf("methodAny(%q) = %#v", method, got)
		}
	}
}

func TestStatusAnyProducesPlainInts(t *testing.T) {
	for _, code := range []int{200, 204, 300, 301, 404, 418, 429, 500, 503, 599, 0, 9999} {
		got := statusAny(code)
		v, ok := got.(int)
		if !ok {
			t.Fatalf("statusAny(%d) dynamic type = %T, want int", code, got)
		}
		if v != code {
			t.Fatalf("statusAny(%d) = %d", code, v)
		}
	}
}

func TestFinalizeRequestFieldsRemainPlainValues(t *testing.T) {
	// 404 is a client error: the operation outcome stays "success"
	// (resolveOutcome only classifies >= 500 as failure).
	sink := hc.NewTestSink()
	cfg := hc.Config{Sink: sink, SamplingRate: 1}

	ctx, event := StartRequest(context.Background(), "GET", "/orders")
	FinalizeRequest(cfg, FinalizeInput{
		Ctx:        ctx,
		Event:      event,
		StatusCode: 404,
	})

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("captured %d events, want 1", len(events))
	}
	fields := events[0].Fields

	for key, want := range map[string]any{
		"http.method": "GET",
		"http.path":   "/orders",
		"http.status": 404,
		"op.domain":   "http",
		"op.outcome":  "success",
	} {
		got, ok := fields[key]
		if !ok {
			t.Fatalf("missing field %q", key)
		}
		if got != want {
			t.Fatalf("field %q = %#v (%T), want %#v", key, got, got, want)
		}
		if _, isStr := got.(string); isStr == (want == 404) {
			t.Fatalf("field %q dynamic type = %T", key, got)
		}
	}

	// 5xx does map to the failure outcome; still plain strings.
	sink2 := hc.NewTestSink()
	cfg2 := hc.Config{Sink: sink2, SamplingRate: 1}
	ctx2, event2 := StartRequest(context.Background(), "POST", "/pay")
	FinalizeRequest(cfg2, FinalizeInput{
		Ctx:        ctx2,
		Event:      event2,
		StatusCode: 503,
	})
	events2 := sink2.Events()
	if len(events2) != 1 {
		t.Fatalf("captured %d events, want 1", len(events2))
	}
	if got := events2[0].Fields["op.outcome"]; got != any("failure") {
		t.Fatalf("op.outcome = %#v, want \"failure\"", got)
	}
	if got := events2[0].Fields["http.status"]; got != any(503) {
		t.Fatalf("http.status = %#v, want 503", got)
	}
}
