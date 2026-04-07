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
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, event := NewContext(baseCtx)
	applyOperationStartFields(ctx, start)
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
	cfg = NormalizeConfig(cfg)
	if cfg.Sink == nil || event == nil || ctx == nil {
		return false
	}

	policy := policyForDomain(cfg, start.Domain)

	applyOperationStartFields(ctx, start)
	outcome := resolveOutcome(result)
	annotateOperationFailures(event, result.Err, result.Recovered)

	duration := time.Since(event.startedAt())
	annotateOperationCompletion(event, duration, result.Code, outcome)

	snap := event.snapshot()

	autoLevel := levelFromPolicy(policy, outcome)
	level := MergeLevelWithFloor(autoLevel, snap.level, snap.hasLevel)

	sampleIn := buildSampleInput(event, snap, start, result, duration, outcome, level)
	if !shouldWriteOperation(cfg, policy, sampleIn) {
		return false
	}

	cfg.Sink.Write(level, resolveEventMessage(cfg.Message, start.Domain, snap.message), snap.fields)
	return true
}

func applyOperationStartFields(ctx context.Context, start OperationStart) {
	event := FromContext(ctx)
	if event == nil {
		return
	}
	start = normalizedOperationStart(start)
	if !operationStartFieldsNeedUpdate(event, start) {
		return
	}

	capHint := 2
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
	event.fields["op.domain"] = string(start.Domain)
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
	event.fields["op.outcome"] = string(outcome)
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

func buildSampleInput(event *Event, snap snapshot, start OperationStart, result operationResult, duration time.Duration, outcome Outcome, level Level) SampleInput {
	fields := snap.fields

	method, _ := fields["http.method"].(string)
	path, _ := fields["http.path"].(string)
	statusCode := result.Code
	if v, ok := fields["http.status"]; ok {
		if parsed, ok := asInt(v); ok {
			statusCode = parsed
		}
	}

	hasError := snap.hasError || result.Code >= 500 || outcome != OutcomeSuccess

	name := start.Name
	if name == "" {
		name = defaultOpName
	}

	return SampleInput{
		Domain:     normalizeDomain(start.Domain),
		Operation:  name,
		Outcome:    outcome,
		Code:       result.Code,
		Method:     method,
		Path:       path,
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
