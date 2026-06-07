package hc

import "time"

// EventFields returns a shallow-copied field snapshot for e.
// Nested map/slice values are shared by reference.
func EventFields(e *Event) map[string]any {
	if e == nil {
		return nil
	}
	return e.fieldsSnapshot()
}

// EventMessage returns e's attached message, or an empty string if unset.
func EventMessage(e *Event) string {
	if e == nil {
		return ""
	}
	return e.getMessage()
}

// EventHasError reports whether e has an attached error.
func EventHasError(e *Event) bool {
	if e == nil {
		return false
	}
	return e.hasErrorValue()
}

// EventHasMessage reports whether e has an attached message.
func EventHasMessage(e *Event) bool {
	if e == nil {
		return false
	}
	return e.hasMessageValue()
}

// EventStartTime returns e's wall-clock start time.
//
// Local, in-place, and pooled integration APIs use monotonic-only timing for
// duration and return a zero wall-clock start time.
func EventStartTime(e *Event) time.Time {
	if e == nil {
		return time.Time{}
	}
	return e.startedAt()
}
