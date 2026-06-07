package hc

import "context"

var backgroundContext = context.Background()

// PreparedOperationStart stores normalized operation metadata for repeated use.
type PreparedOperationStart struct {
	start OperationStart
}

// PrepareOperationStart normalizes operation metadata once for repeated use.
func PrepareOperationStart(start OperationStart) PreparedOperationStart {
	return PreparedOperationStart{start: normalizedOperationStart(start)}
}

// OperationStart returns the normalized operation metadata.
func (prepared PreparedOperationStart) OperationStart() OperationStart {
	return prepared.start
}

// StartLocalContext starts a local operation with ctx.
func (prepared PreparedOperationStart) StartLocalContext(ctx context.Context) Operation {
	if ctx == nil {
		ctx = backgroundContext
	}
	event := newLocalEvent()
	return Operation{
		ctx:              ctx,
		event:            event,
		start:            prepared.start,
		startMono:        monotonicNow(),
		deferStartFields: true,
	}
}

// StartLocalNoTimingContext starts a local operation with ctx and disables
// duration timing. Finalized events emit duration_ms as 0.
func (prepared PreparedOperationStart) StartLocalNoTimingContext(ctx context.Context) Operation {
	if ctx == nil {
		ctx = backgroundContext
	}
	event := newLocalEvent()
	return Operation{
		ctx:              ctx,
		event:            event,
		start:            prepared.start,
		deferStartFields: true,
		noTiming:         true,
	}
}

// StartInPlaceContext starts an in-place operation with ctx.
//
// The provided event must not be used concurrently with another active
// operation or goroutine.
func (prepared PreparedOperationStart) StartInPlaceContext(ctx context.Context, event *Event) Operation {
	if event == nil {
		return prepared.StartLocalContext(ctx)
	}
	if ctx == nil {
		ctx = backgroundContext
	}
	event.resetLocal()
	return Operation{
		ctx:              ctx,
		event:            event,
		start:            prepared.start,
		startMono:        monotonicNow(),
		deferStartFields: true,
		unsafeEvent:      true,
	}
}

// StartInPlaceNoTimingContext starts an in-place operation with ctx and
// disables duration timing. Finalized events emit duration_ms as 0.
//
// The provided event must not be used concurrently with another active
// operation or goroutine.
func (prepared PreparedOperationStart) StartInPlaceNoTimingContext(ctx context.Context, event *Event) Operation {
	if event == nil {
		return prepared.StartLocalNoTimingContext(ctx)
	}
	if ctx == nil {
		ctx = backgroundContext
	}
	event.resetLocal()
	return Operation{
		ctx:              ctx,
		event:            event,
		start:            prepared.start,
		deferStartFields: true,
		unsafeEvent:      true,
		noTiming:         true,
	}
}
