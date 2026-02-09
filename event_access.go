package hc

import "time"

// EventFields returns a deep-copied immutable field snapshot for e.
func EventFields(e *Event) map[string]any {
	if e == nil {
		return nil
	}
	return e.snapshot().fields
}

// EventHasError reports whether e has an attached error.
func EventHasError(e *Event) bool {
	if e == nil {
		return false
	}
	return e.hasErrorValue()
}

// EventStartTime returns e's start time.
func EventStartTime(e *Event) time.Time {
	if e == nil {
		return time.Time{}
	}
	return e.startedAt()
}
