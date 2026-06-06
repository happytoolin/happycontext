package hc

import (
	"errors"
	"testing"
	"time"
)

func TestStructuredErrorFieldPreservesWrappedErrorContext(t *testing.T) {
	field := structuredErrorField(wrappedError{err: errors.New("boom")})

	if field["message"] != "wrapped: boom" {
		t.Fatalf("message = %v, want wrapped message", field["message"])
	}
	if field["type"] != "hc.wrappedError" {
		t.Fatalf("type = %v, want hc.wrappedError", field["type"])
	}
	if field["cause.message"] != "boom" {
		t.Fatalf("cause.message = %v, want boom", field["cause.message"])
	}
	if field["cause.type"] != "*errors.errorString" {
		t.Fatalf("cause.type = %v, want *errors.errorString", field["cause.type"])
	}
}

func TestStructuredErrorFieldNormalizesFrameworkStyleErrors(t *testing.T) {
	field := structuredErrorField(&frameworkStyleError{Code: 500, Message: "boom"})

	if field["message"] != "boom" {
		t.Fatalf("message = %v, want boom", field["message"])
	}
	if field["type"] != "*hc.frameworkStyleError" {
		t.Fatalf("type = %v, want *hc.frameworkStyleError", field["type"])
	}
	if _, ok := field["cause.message"]; ok {
		t.Fatalf("did not expect cause.message for direct framework error")
	}
	if _, ok := field["cause.type"]; ok {
		t.Fatalf("did not expect cause.type for direct framework error")
	}
}

func TestStructuredErrorFieldNormalizesFrameworkStyleDeepestCause(t *testing.T) {
	field := structuredErrorField(wrappedError{err: &frameworkStyleError{Code: 500, Message: "boom"}})

	if field["message"] != "wrapped: code=500, message=boom" {
		t.Fatalf("message = %v, want wrapped framework message", field["message"])
	}
	if field["type"] != "hc.wrappedError" {
		t.Fatalf("type = %v, want hc.wrappedError", field["type"])
	}
	if field["cause.message"] != "boom" {
		t.Fatalf("cause.message = %v, want boom", field["cause.message"])
	}
	if field["cause.type"] != "*hc.frameworkStyleError" {
		t.Fatalf("cause.type = %v, want *hc.frameworkStyleError", field["cause.type"])
	}
}

func TestStructuredErrorFieldHandlesCyclicUnwrap(t *testing.T) {
	err := &cyclicError{}
	done := make(chan map[string]any, 1)
	go func() {
		done <- structuredErrorField(err)
	}()

	select {
	case field := <-done:
		if field["message"] != "cycle" {
			t.Fatalf("message = %v, want cycle", field["message"])
		}
		if _, ok := field["cause.message"]; ok {
			t.Fatal("did not expect cause.message for self-unwrapping error")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("structuredErrorField did not return for cyclic unwrap")
	}
}

type cyclicError struct{}

func (c *cyclicError) Error() string {
	return "cycle"
}

func (c *cyclicError) Unwrap() error {
	return c
}
