package hc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// StartOperation initializes a stateful operation handle for non-HTTP flows.
func StartOperation(baseCtx context.Context, start OperationStart) *Operation {
	ctx, event := NewContext(baseCtx)
	return &Operation{
		ctx:              ctx,
		event:            event,
		start:            normalizedOperationStart(start),
		deferStartFields: true,
	}
}

// beginOperation initializes context/event and operation envelope metadata.
//
// beginOperation is a low-level helper used by package integrations.
func beginOperation(baseCtx context.Context, start OperationStart) (context.Context, *Event) {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
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
	}, op.deferStartFields, op.startMono, op.unsafeEvent, op.noTiming)
	if recovered != nil {
		panic(recovered)
	}
	return wrote
}

// Finish finalizes an operation without panic capture.
//
// Use End when deferring operation completion and preserving panic metadata.
// Use Finish in hot paths where the caller handles errors explicitly and does
// not need happycontext to recover and re-panic.
func (op *Operation) Finish(cfg Config, err error) bool {
	if op == nil {
		return false
	}
	return finishOperation(cfg, op.ctx, op.event, op.start, operationResult{
		Err: err,
	}, op.deferStartFields, op.startMono, op.unsafeEvent, op.noTiming)
}

// FinishOperation finalizes and writes an operation event.
//
// FinishOperation is a low-level helper used by package integrations.
func FinishOperation(cfg Config, in OperationFinish) bool {
	return finishPreparedOperation(prepareConfig(cfg), in)
}

// finishPreparedOperation finalizes and writes an operation event using a
// prepared config. If in.StartComplete is false, missing operation metadata is
// merged from the event for compatibility with beginOperation callers.
func finishPreparedOperation(prepared preparedConfig, in OperationFinish) bool {
	cfg := prepared.cfg
	if cfg.Sink == nil || in.Event == nil || in.Ctx == nil {
		return false
	}
	start := in.Start
	if !in.StartComplete {
		start = hydrateOperationStart(start, in.Event)
	}
	if in.StartComplete {
		if handled, wrote := finishPreparedCompleteDefaultOperation(prepared, in.Event, start, operationResult{
			Outcome:   in.Outcome,
			Code:      in.Code,
			Err:       in.Err,
			Recovered: in.Recovered,
		}, in.UnsafeEvent); handled {
			return wrote
		}
	}
	return finishOperationPrepared(prepared, in.Ctx, in.Event, start, operationResult{
		Outcome:   in.Outcome,
		Code:      in.Code,
		Err:       in.Err,
		Recovered: in.Recovered,
	}, true, 0, in.UnsafeEvent, false)
}

func finishOperation(cfg Config, ctx context.Context, event *Event, start OperationStart, result operationResult, deferStartFields bool, startMono int64, unsafeEvent bool, noTiming bool) bool {
	return finishOperationPrepared(prepareConfig(cfg), ctx, event, start, result, deferStartFields, startMono, unsafeEvent, noTiming)
}

func finishOperationPrepared(prepared preparedConfig, ctx context.Context, event *Event, start OperationStart, result operationResult, deferStartFields bool, startMono int64, unsafeEvent bool, noTiming bool) bool {
	cfg := prepared.cfg
	if cfg.Sink == nil || event == nil || ctx == nil {
		return false
	}

	if handled, wrote := finishPreparedDefaultFastPath(prepared, event, start, result, deferStartFields, unsafeEvent, noTiming); handled {
		return wrote
	}

	policy, hasPolicy := prepared.policyForDomain(start.Domain)

	outcome := resolveOutcome(result)
	if result.Err != nil || result.Recovered != nil {
		annotateOperationFailures(event, result.Err, result.Recovered)
	}

	needsPreSamplingFields := cfg.Sampler != nil || len(cfg.Enrichers) > 0
	useUnsafeEvent := unsafeEvent && !needsPreSamplingFields
	needsFieldState := needsPreSamplingFields || normalizeDomain(start.Domain) == DomainHTTP
	var state eventState
	if needsFieldState {
		state = event.state()
	} else if useUnsafeEvent {
		state = event.baseStateUnsafe()
	} else {
		state = event.baseState()
	}
	if needsPreSamplingFields {
		applyOperationStartFieldsToEvent(event, start)
		state = event.state()
	} else if !deferStartFields {
		applyOperationStartFieldsToEvent(event, start)
	}
	duration := time.Duration(0)
	durationReady := noTiming
	if needsPreSamplingFields {
		if !noTiming {
			duration = durationSinceState(state, startMono)
		}
		durationReady = true
		annotateOperationCompletion(event, duration, result.Code, outcome)
		applyEnrichers(cfg.Enrichers, ctx, event)
		state = event.state()
	}

	autoLevel := defaultLevelForOutcome(outcome)
	if hasPolicy {
		autoLevel = levelFromPolicy(policy, outcome)
	}
	level := MergeLevelWithFloor(autoLevel, state.level, state.hasLevel)

	if cfg.Sampler != nil {
		sampleIn := buildSampleInput(event, state, start, result, duration, outcome, level)
		if !cfg.Sampler(sampleIn) {
			return false
		}
	} else {
		statusCode := result.Code
		if state.hasStatus {
			statusCode = state.statusCode
		}
		hasError := state.hasError || result.Code >= 500 || outcome != OutcomeSuccess
		if !shouldWriteOperationDefaultPrepared(prepared, policy, hasError, result.Code, statusCode, outcome, level) {
			return false
		}
	}
	if !durationReady {
		duration = durationSinceState(state, startMono)
	}
	message := resolveEventMessage(cfg.Message, start.Domain, state.message)
	if needsPreSamplingFields {
		writeEventToSink(cfg.Sink, event, level, message, cfg.FieldMapper)
		return true
	}

	if deferStartFields {
		if useUnsafeEvent {
			writeEventToSinkWithStartAndCompletionUnsafe(cfg.Sink, event, level, message, start, duration.Milliseconds(), result.Code, outcome, cfg.FieldMapper)
			return true
		}
		writeEventToSinkWithStartAndCompletion(cfg.Sink, event, level, message, start, duration.Milliseconds(), result.Code, outcome, cfg.FieldMapper)
		return true
	}

	writeEventToSinkWithCompletion(cfg.Sink, event, level, message, duration.Milliseconds(), result.Code, outcome, cfg.FieldMapper)
	return true
}

func finishPreparedDefaultFastPath(prepared preparedConfig, event *Event, start OperationStart, result operationResult, deferStartFields bool, unsafeEvent bool, noTiming bool) (bool, bool) {
	if !prepared.fastDefaultOperation || !unsafeEvent || !deferStartFields || !noTiming {
		return false, false
	}
	if result.Outcome != "" || result.Err != nil || result.Recovered != nil || result.Code >= 500 {
		return false, false
	}
	return writePreparedDefaultSuccessNoTiming(prepared, event, start, result.Code)
}

func writePreparedDefaultSuccessNoTiming(prepared preparedConfig, event *Event, start OperationStart, code int) (bool, bool) {
	if !prepared.fastDefaultOperation || start.Domain == DomainHTTP || code >= 500 {
		return false, false
	}
	if event.hasLifecycleOverridesUnsafe() {
		return false, false
	}

	rate := prepared.cfg.SamplingRate
	if rate <= 0 {
		return true, false
	}
	if rate < 1 && !shouldSample(rate) {
		return true, false
	}

	message := prepared.cfg.Message
	if message == "" {
		message = DefaultOperationMessage
	}
	writeEventToSinkWithStartAndCompletionUnsafe(prepared.cfg.Sink, event, LevelInfo, message, start, 0, code, OutcomeSuccess, nil)
	return true, true
}

func finishPreparedCompleteDefaultOperation(prepared preparedConfig, event *Event, start OperationStart, result operationResult, unsafeEvent bool) (bool, bool) {
	if !prepared.fastDefaultOperation {
		return false, false
	}
	if unsafeEvent {
		if handled, wrote := writePreparedCompleteDefaultSuccessFast(prepared, event, start, result); handled {
			return true, wrote
		}
	}

	outcome := resolveOutcome(result)
	if result.Err != nil || result.Recovered != nil {
		annotateOperationFailures(event, result.Err, result.Recovered)
	}

	var state eventState
	if unsafeEvent {
		state = event.baseStateUnsafe()
	} else {
		state = event.baseState()
	}
	autoLevel := defaultLevelForOutcome(outcome)
	level := MergeLevelWithFloor(autoLevel, state.level, state.hasLevel)
	hasError := state.hasError || result.Code >= 500 || outcome != OutcomeSuccess
	if !shouldWriteOperationDefaultPrepared(prepared, OperationPolicy{}, hasError, result.Code, result.Code, outcome, level) {
		return true, false
	}

	duration := durationSinceState(state, 0)
	message := resolveEventMessage(prepared.cfg.Message, start.Domain, state.message)
	if unsafeEvent {
		writeEventToSinkWithStartAndCompletionUnsafe(prepared.cfg.Sink, event, level, message, start, duration.Milliseconds(), result.Code, outcome, nil)
		return true, true
	}
	writeEventToSinkWithStartAndCompletion(prepared.cfg.Sink, event, level, message, start, duration.Milliseconds(), result.Code, outcome, nil)
	return true, true
}

func writePreparedCompleteDefaultSuccessFast(prepared preparedConfig, event *Event, start OperationStart, result operationResult) (bool, bool) {
	if result.Outcome != "" || result.Err != nil || result.Recovered != nil || result.Code >= 500 {
		return false, false
	}
	if event.hasLifecycleOverridesUnsafe() {
		return false, false
	}

	rate := prepared.cfg.SamplingRate
	if rate <= 0 {
		return true, false
	}
	if rate < 1 && !shouldSample(rate) {
		return true, false
	}

	message := prepared.cfg.Message
	if message == "" {
		message = resolveMessage("", start.Domain)
	}
	durationMS := int64(0)
	if event.startMono != 0 {
		durationMS = (monotonicNow() - event.startMono) / int64(time.Millisecond)
	} else {
		durationMS = time.Since(event.startTime).Milliseconds()
	}
	writeEventToSinkWithStartAndCompletionUnsafe(prepared.cfg.Sink, event, LevelInfo, message, start, durationMS, result.Code, OutcomeSuccess, nil)
	return true, true
}

func applyEnrichers(enrichers []Enricher, ctx context.Context, event *Event) {
	for _, enricher := range enrichers {
		if enricher == nil {
			continue
		}
		enricher(ctx, event)
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

	event.mu.Lock()
	event.setStringFieldLocked("op.domain", string(start.Domain))
	event.setStringFieldLocked("op.name", start.Name)
	if start.ID != "" {
		event.setStringFieldLocked("op.id", start.ID)
	}
	if start.Source != "" {
		event.setStringFieldLocked("op.source", start.Source)
	}
	if start.Attempt > 0 {
		event.setIntFieldLocked("op.attempt", start.Attempt)
	}
	if start.MaxAttempts > 0 {
		event.setIntFieldLocked("op.max_attempts", start.MaxAttempts)
	}
	event.mu.Unlock()
}

func operationStartFieldsNeedUpdate(event *Event, start OperationStart) bool {
	event.mu.RLock()
	defer event.mu.RUnlock()

	if event.fieldCountLocked() == 0 {
		return true
	}
	if field, _ := event.stringFieldValueLocked("op.domain"); field != string(start.Domain) {
		return true
	}
	if field, _ := event.stringFieldValueLocked("op.name"); field != start.Name {
		return true
	}
	if start.ID != "" {
		if field, _ := event.stringFieldValueLocked("op.id"); field != start.ID {
			return true
		}
	}
	if start.Source != "" {
		if field, _ := event.stringFieldValueLocked("op.source"); field != start.Source {
			return true
		}
	}
	if start.Attempt > 0 {
		if field, ok := event.intFieldValueLocked("op.attempt"); !ok || field != start.Attempt {
			return true
		}
	}
	if start.MaxAttempts > 0 {
		if field, ok := event.intFieldValueLocked("op.max_attempts"); !ok || field != start.MaxAttempts {
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
	if panicField != nil {
		event.setFieldLocked("panic", panicField)
	}
	if errorField != nil {
		event.hasError = true
		event.setFieldLocked("error", errorField)
	}
	event.mu.Unlock()
}

func annotateOperationCompletion(event *Event, duration time.Duration, code int, outcome Outcome) {
	if event == nil {
		return
	}

	event.mu.Lock()
	event.setInt64FieldLocked("duration_ms", duration.Milliseconds())
	event.setIntFieldLocked("op.code", code)
	event.setStringFieldLocked("op.outcome", string(outcome))
	event.mu.Unlock()
}

func resolveOutcome(result operationResult) Outcome {
	if result.Outcome == "" && result.Err == nil && result.Recovered == nil {
		if result.Code >= 500 {
			return OutcomeFailure
		}
		return OutcomeSuccess
	}
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

func buildSampleInput(event *Event, state eventState, start OperationStart, result operationResult, duration time.Duration, outcome Outcome, level Level) SampleInput {
	statusCode := result.Code
	if state.hasStatus {
		statusCode = state.statusCode
	}

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
		StatusCode: statusCode,
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

	if event.fieldCountLocked() == 0 {
		return start
	}

	if start.Domain == "" {
		if v, ok := event.stringFieldValueLocked("op.domain"); ok && v != "" {
			start.Domain = Domain(v)
		}
	}
	if start.Name == "" {
		if v, ok := event.stringFieldValueLocked("op.name"); ok && v != "" {
			start.Name = v
		}
	}
	if start.ID == "" {
		if v, ok := event.stringFieldValueLocked("op.id"); ok {
			start.ID = v
		}
	}
	if start.Source == "" {
		if v, ok := event.stringFieldValueLocked("op.source"); ok {
			start.Source = v
		}
	}
	if start.Attempt == 0 {
		if v, ok := event.intFieldValueLocked("op.attempt"); ok {
			start.Attempt = v
		}
	}
	if start.MaxAttempts == 0 {
		if v, ok := event.intFieldValueLocked("op.max_attempts"); ok {
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
