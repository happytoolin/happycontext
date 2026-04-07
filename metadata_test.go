package hc

import (
	"errors"
	"testing"
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
	if field["type"] != "*errors.errorString" {
		t.Fatalf("type = %v, want *errors.errorString", field["type"])
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
	if field["cause.type"] != "*errors.errorString" {
		t.Fatalf("cause.type = %v, want *errors.errorString", field["cause.type"])
	}
}
