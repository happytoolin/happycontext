package hc

import (
	"errors"
	"fmt"
	"testing"
)

type wrappedTestError struct{ err error }

func (w wrappedTestError) Error() string { return "wrapped: " + w.err.Error() }
func (w wrappedTestError) Unwrap() error { return w.err }

type frameworkStyleTestError struct {
	Code    int
	Message string
}

func (f *frameworkStyleTestError) Error() string {
	return fmt.Sprintf("framework %d: %s", f.Code, f.Message)
}

func TestStructuredErrorField(t *testing.T) {
	field := structuredErrorField(errors.New("boom"))
	if field["message"] != "boom" {
		t.Errorf("message = %v", field["message"])
	}
	if field["type"] != "*errors.errorString" {
		t.Errorf("type = %v", field["type"])
	}
	if _, hasCause := field["cause.message"]; hasCause {
		t.Error("simple error should have no cause")
	}

	wrapped := structuredErrorField(wrappedTestError{err: errors.New("inner")})
	if wrapped["message"] != "wrapped: inner" {
		t.Errorf("message = %v", wrapped["message"])
	}
	if wrapped["cause.message"] != "inner" {
		t.Errorf("cause.message = %v", wrapped["cause.message"])
	}

	fw := structuredErrorField(&frameworkStyleTestError{Code: 500, Message: "kaput"})
	if fw["message"] != "kaput" {
		t.Errorf("framework message = %v", fw["message"])
	}
}

func TestStructuredPanicField(t *testing.T) {
	field := structuredPanicField("boom")
	if field["value"] != "boom" || field["type"] != "string" {
		t.Fatalf("panic field = %v", field)
	}
}

func TestCyclicErrorUnwrap(t *testing.T) {
	a := &cyclicError{}
	b := &cyclicError{next: a}
	a.next = b
	field := structuredErrorField(a)
	if field == nil {
		t.Fatal("cyclic unwrap must terminate")
	}
}

type cyclicError struct{ next error }

func (c *cyclicError) Error() string { return "cyclic" }
func (c *cyclicError) Unwrap() error { return c.next }
