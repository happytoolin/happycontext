package hc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// StartOperation initializes a stateful operation handle for non-HTTP flows.
func StartOperation(baseCtx context.Context, start OperationStart) *Operation {
	ctx, event := BeginOperation(baseCtx, start)
	return &Operation{
		ctx:   ctx,
		event: event,
		start: normalizedOperationStart(start),
	}
}

// BeginOperation initializes context/event and operation envelope metadata.
//
// BeginOperation is a low-level helper used by package integrations.
func BeginOperation(baseCtx context.Context, start OperationStart) (context.Context, *Event) {
	ctx, event := NewContext(baseCtx)
	applyOperationStartFieldsToEvent(event, start)
	return ctx, event
}

// Context returns the operation context.
func (op *Operation) Context() context.Context {
	if op == nil {
		return nil
	}
	return op.ctx
}

// Event returns the underlying event.
func (op *Operation) Event() *Event {
	if op == nil {
		return nil
	}
	return op.event
}

// End finalizes an operation using the current function's error return and panic state.
//
// End is intended for deferred use:
//
//	func run() (err error) {
//		op := StartOperation(ctx, start)
//		defer op.End(cfg, &err)
//		...
//		return err
//	}
//
// If the surrounding function is panicking, End records the panic and then re-panics.
func (op *Operation) End(cfg Config, errp *error) bool {
	if op == nil {
		return false
	}
	var err error
	if errp != nil {
		err = *errp
	}
	recovered := recover()
	wrote := finishOperation(cfg, op.ctx, op.event, op.start, operationResult{
		Err:       err,
		Recovered: recovered,
	})
	if recovered != nil {
		panic(recovered)
	}
	return wrote
}

// FinishOperation finalizes and writes an operation event.
//
// FinishOperation is a low-level helper used by package integrations.
func FinishOperation(cfg Config, in OperationFinish) bool {
	return finishOperation(cfg, in.Ctx, in.Event, hydrateOperationStart(in.Start, in.Event), operationResult{
		Outcome:   in.Outcome,
		Code:      in.Code,
		Err:       in.Err,
		Recovered: in.Recovered,
	})
}

func finishOperation(cfg Config, ctx context.Context, event *Event, start OperationStart, result operationResult) bool {
	if cfg.Sink == nil || event == nil || ctx == nil {
		return false
	}

	policy := policyForDomain(cfg, start.Domain)

	applyOperationStartFieldsToEvent(event, start)
	outcome := resolveOutcome(result)
	annotateOperationFailures(event, result.Err, result.Recovered)

	duration := time.Since(event.startedAt())
	annotateOperationCompletion(event, duration, result.Code, outcome)

	autoLevel := levelFromPolicy(policy, outcome)

	var snap snapshot
	var sampleState operationSampleState
	if cfg.Sampler != nil {
		snap = event.snapshot()
		sampleState = operationSampleStateFromSnapshot(snap, result.Code)
	} else {
		sampleState = operationSampleStateFromEvent(event, result.Code)
	}
	level := MergeLevelWithFloor(autoLevel, sampleState.level, sampleState.hasLevel)

	sampleIn := buildSampleInput(event, sampleState, start, result, duration, outcome, level)
	if !shouldWriteOperation(cfg, policy, sampleIn) {
		return false
	}
	if cfg.Sampler == nil {
		snap = event.snapshot()
	}

	cfg.Sink.Write(level, resolveEventMessage(cfg.Message, start.Domain, snap.message), snap.fields)
	return true
}

// Pre-boxed field values for the library's fixed strings. Storing these in
// map[string]any avoids a heap allocation per write; dynamic values fall back
// to a fresh conversion.
var (
	domainHTTPAny      = any(string(DomainHTTP))
	domainJobAny       = any(string(DomainJob))
	domainMessageAny   = any(string(DomainMessage))
	domainCLIAny       = any(string(DomainCLI))
	domainDefaultAny   = any(string(defaultDomainValue))
	outcomeSuccessAny  = any(string(OutcomeSuccess))
	outcomeFailureAny  = any(string(OutcomeFailure))
	outcomePanicAny    = any(string(OutcomePanic))
	outcomeCanceledAny = any(string(OutcomeCanceled))
	outcomeTimeoutAny  = any(string(OutcomeTimeout))
	outcomeRetryAny    = any(string(OutcomeRetry))
)

func domainAny(domain Domain) any {
	switch domain {
	case DomainHTTP:
		return domainHTTPAny
	case DomainJob:
		return domainJobAny
	case DomainMessage:
		return domainMessageAny
	case DomainCLI:
		return domainCLIAny
	case defaultDomainValue:
		return domainDefaultAny
	default:
		return string(domain)
	}
}

func outcomeAny(outcome Outcome) any {
	switch outcome {
	case OutcomeSuccess:
		return outcomeSuccessAny
	case OutcomeFailure:
		return outcomeFailureAny
	case OutcomePanic:
		return outcomePanicAny
	case OutcomeCanceled:
		return outcomeCanceledAny
	case OutcomeTimeout:
		return outcomeTimeoutAny
	case OutcomeRetry:
		return outcomeRetryAny
	default:
		return string(outcome)
	}
}

func applyOperationStartFieldsToEvent(event *Event, start OperationStart) {
	if event == nil {
		return
	}
	start = normalizedOperationStart(start)
	if !operationStartFieldsNeedUpdate(event, start) {
		return
	}

	capHint := 5
	if start.ID != "" {
		capHint++
	}
	if start.Source != "" {
		capHint++
	}
	if start.Attempt > 0 {
		capHint++
	}
	if start.MaxAttempts > 0 {
		capHint++
	}

	event.mu.Lock()
	if event.fields == nil {
		event.fields = make(map[string]any, max(capHint, 8))
	}
	event.fields["op.domain"] = domainAny(start.Domain)
	event.fields["op.name"] = start.Name
	if start.ID != "" {
		event.fields["op.id"] = start.ID
	}
	if start.Source != "" {
		event.fields["op.source"] = start.Source
	}
	if start.Attempt > 0 {
		event.fields["op.attempt"] = start.Attempt
	}
	if start.MaxAttempts > 0 {
		event.fields["op.max_attempts"] = start.MaxAttempts
	}
	event.mu.Unlock()
}

func operationStartFieldsNeedUpdate(event *Event, start OperationStart) bool {
	event.mu.RLock()
	defer event.mu.RUnlock()

	if len(event.fields) == 0 {
		return true
	}
	if field, _ := event.fields["op.domain"].(string); field != string(start.Domain) {
		return true
	}
	if field, _ := event.fields["op.name"].(string); field != start.Name {
		return true
	}
	if start.ID != "" {
		if field, _ := event.fields["op.id"].(string); field != start.ID {
			return true
		}
	}
	if start.Source != "" {
		if field, _ := event.fields["op.source"].(string); field != start.Source {
			return true
		}
	}
	if start.Attempt > 0 {
		if field, ok := asInt(event.fields["op.attempt"]); !ok || field != start.Attempt {
			return true
		}
	}
	if start.MaxAttempts > 0 {
		if field, ok := asInt(event.fields["op.max_attempts"]); !ok || field != start.MaxAttempts {
			return true
		}
	}
	return false
}

func annotateOperationFailures(event *Event, err error, recovered any) {
	if event == nil {
		return
	}

	var panicField map[string]any
	if recovered != nil {
		panicField = structuredPanicField(recovered)
	}

	var errorField map[string]any
	switch {
	case err != nil:
		errorField = structuredErrorField(err)
	case recovered != nil:
		errorField = structuredErrorField(fmt.Errorf("panic: %v", recovered))
	}

	if panicField == nil && errorField == nil {
		return
	}

	event.mu.Lock()
	if event.fields == nil {
		event.fields = make(map[string]any, 8)
	}
	if panicField != nil {
		event.fields["panic"] = panicField
	}
	if errorField != nil {
		event.hasError = true
		event.fields["error"] = errorField
	}
	event.mu.Unlock()
}

func annotateOperationCompletion(event *Event, duration time.Duration, code int, outcome Outcome) {
	if event == nil {
		return
	}

	event.mu.Lock()
	if event.fields == nil {
		event.fields = make(map[string]any, 8)
	}
	event.fields["duration_ms"] = duration.Milliseconds()
	event.fields["op.code"] = code
	event.fields["op.outcome"] = outcomeAny(outcome)
	event.mu.Unlock()
}

func resolveOutcome(result operationResult) Outcome {
	if IsValidOutcome(result.Outcome) {
		return result.Outcome
	}
	if result.Recovered != nil {
		return OutcomePanic
	}
	if result.Err != nil {
		switch {
		case errors.Is(result.Err, context.Canceled):
			return OutcomeCanceled
		case errors.Is(result.Err, context.DeadlineExceeded):
			return OutcomeTimeout
		default:
			return OutcomeFailure
		}
	}
	if result.Code >= 500 {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

type operationSampleState struct {
	method     string
	path       string
	statusCode int
	hasError   bool
	level      Level
	hasLevel   bool
}

func operationSampleStateFromFields(fields map[string]any, defaultStatus int) operationSampleState {
	state := operationSampleState{statusCode: defaultStatus}
	state.method, _ = fields["http.method"].(string)
	state.path, _ = fields["http.path"].(string)
	if status, ok := asInt(fields["http.status"]); ok {
		state.statusCode = status
	}
	return state
}

func operationSampleStateFromSnapshot(snap snapshot, defaultStatus int) operationSampleState {
	state := operationSampleStateFromFields(snap.fields, defaultStatus)
	state.hasError = snap.hasError
	state.level = snap.level
	state.hasLevel = snap.hasLevel
	return state
}

func operationSampleStateFromEvent(event *Event, defaultStatus int) operationSampleState {
	event.mu.RLock()
	defer event.mu.RUnlock()

	state := operationSampleStateFromFields(event.fields, defaultStatus)
	state.hasError = event.hasError
	state.level = event.requestedLevel
	state.hasLevel = event.hasRequestedLevel
	return state
}

func buildSampleInput(event *Event, state operationSampleState, start OperationStart, result operationResult, duration time.Duration, outcome Outcome, level Level) SampleInput {
	hasError := state.hasError || result.Code >= 500 || outcome != OutcomeSuccess

	name := start.Name
	if name == "" {
		name = defaultOpName
	}

	return SampleInput{
		Domain:     normalizeDomain(start.Domain),
		Operation:  name,
		Outcome:    outcome,
		Code:       result.Code,
		Method:     state.method,
		Path:       state.path,
		StatusCode: state.statusCode,
		Duration:   duration,
		Level:      level,
		HasError:   hasError,
		Event:      event,
	}
}

func hydrateOperationStart(start OperationStart, event *Event) OperationStart {
	if event == nil {
		return start
	}

	event.mu.RLock()
	defer event.mu.RUnlock()

	if len(event.fields) == 0 {
		return start
	}

	if start.Domain == "" {
		if v, ok := event.fields["op.domain"].(string); ok && v != "" {
			start.Domain = Domain(v)
		}
	}
	if start.Name == "" {
		if v, ok := event.fields["op.name"].(string); ok && v != "" {
			start.Name = v
		}
	}
	if start.ID == "" {
		if v, ok := event.fields["op.id"].(string); ok {
			start.ID = v
		}
	}
	if start.Source == "" {
		if v, ok := event.fields["op.source"].(string); ok {
			start.Source = v
		}
	}
	if start.Attempt == 0 {
		if v, ok := asInt(event.fields["op.attempt"]); ok {
			start.Attempt = v
		}
	}
	if start.MaxAttempts == 0 {
		if v, ok := asInt(event.fields["op.max_attempts"]); ok {
			start.MaxAttempts = v
		}
	}
	return start
}

func resolveMessage(configured string, domain Domain) string {
	if configured != "" {
		return configured
	}
	if normalizeDomain(domain) == DomainHTTP {
		return DefaultMessage
	}
	return DefaultOperationMessage
}

func resolveEventMessage(configured string, domain Domain, eventMessage string) string {
	if eventMessage != "" {
		return eventMessage
	}
	return resolveMessage(configured, domain)
}

func normalizedOperationStart(start OperationStart) OperationStart {
	start.Domain = normalizeDomain(start.Domain)
	if start.Name == "" {
		start.Name = defaultOpName
	}
	return start
}
