package hc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultDomainValue Domain = "operation"
	defaultOpName             = "operation"
)

// Domain identifies the operation category.
type Domain string

const (
	DomainHTTP    Domain = "http"
	DomainJob     Domain = "job"
	DomainMessage Domain = "msg"
	DomainCLI     Domain = "cli"
)

// Outcome describes operation completion status.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeFailure  Outcome = "failure"
	OutcomePanic    Outcome = "panic"
	OutcomeCanceled Outcome = "canceled"
	OutcomeTimeout  Outcome = "timeout"
	OutcomeRetry    Outcome = "retry"
)

// OperationStart describes operation metadata initialized at start.
type OperationStart struct {
	Domain      Domain
	Name        string
	ID          string
	Source      string
	Attempt     int
	MaxAttempts int
}

// OperationResult contains finish-time outcome data for one operation event.
type OperationResult struct {
	Outcome   Outcome
	Code      int
	Err       error
	Recovered any
}

// Operation provides a stateful non-HTTP lifecycle handle.
type Operation struct {
	ctx   context.Context
	event *Event
	start OperationStart
}

// OperationFinish contains inputs required to finalize one operation event.
type OperationFinish struct {
	Ctx       context.Context
	Event     *Event
	Start     OperationStart
	Outcome   Outcome
	Code      int
	Err       error
	Recovered any
}

// OperationPolicy customizes lifecycle defaults per domain.
type OperationPolicy struct {
	SuccessLevel  Level
	FailureLevel  Level
	PanicLevel    Level
	OutcomeLevels map[Outcome]Level
	SamplingRate  *float64
}

// StartOperation initializes a stateful operation handle for non-HTTP flows.
func StartOperation(baseCtx context.Context, start OperationStart) *Operation {
	ctx, event := BeginOperation(baseCtx, start)
	return &Operation{
		ctx:   ctx,
		event: event,
		start: hydrateOperationStart(start, event),
	}
}

// BeginOperation initializes context/event and operation envelope metadata.
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

// Finish finalizes and writes one operation event.
func (op *Operation) Finish(cfg Config, result OperationResult) bool {
	if op == nil {
		return false
	}
	return finishOperation(cfg, op.ctx, op.event, op.start, result)
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
	var err error
	if errp != nil {
		err = *errp
	}
	recovered := recover()
	wrote := op.Finish(cfg, OperationResult{
		Err:       err,
		Recovered: recovered,
	})
	if recovered != nil {
		panic(recovered)
	}
	return wrote
}

// FinishOperation finalizes and writes an operation event.
func FinishOperation(cfg Config, in OperationFinish) bool {
	return finishOperation(cfg, in.Ctx, in.Event, hydrateOperationStart(in.Start, in.Event), OperationResult{
		Outcome:   in.Outcome,
		Code:      in.Code,
		Err:       in.Err,
		Recovered: in.Recovered,
	})
}

func finishOperation(cfg Config, ctx context.Context, event *Event, start OperationStart, result OperationResult) bool {
	if cfg.Sink == nil || event == nil || ctx == nil {
		return false
	}

	policy := policyForDomain(cfg, start.Domain)

	applyOperationStartFields(ctx, start)
	annotateOperationFailures(ctx, result.Err, result.Recovered)

	duration := time.Since(EventStartTime(event))
	Add(ctx, "duration_ms", duration.Milliseconds(), "op.code", result.Code)

	outcome := resolveOutcome(result)
	Add(ctx, "op.outcome", string(outcome))

	autoLevel := levelFromPolicy(policy, outcome)
	requestedLevel, hasRequestedLevel := GetLevel(ctx)
	level := mergeLevelWithFloor(autoLevel, requestedLevel, hasRequestedLevel)

	sampleIn := buildSampleInput(event, start, result, duration, outcome, level)
	if !shouldWriteOperation(cfg, policy, sampleIn) {
		return false
	}

	cfg.Sink.Write(level, resolveEventMessage(cfg.Message, start.Domain, event), EventFields(event))
	return true
}

func applyOperationStartFields(ctx context.Context, start OperationStart) {
	domain := normalizeDomain(start.Domain)
	name := start.Name
	if name == "" {
		name = defaultOpName
	}

	kv := []any{
		"op.domain", string(domain),
		"op.name", name,
	}
	if start.ID != "" {
		kv = append(kv, "op.id", start.ID)
	}
	if start.Source != "" {
		kv = append(kv, "op.source", start.Source)
	}
	if start.Attempt > 0 {
		kv = append(kv, "op.attempt", start.Attempt)
	}
	if start.MaxAttempts > 0 {
		kv = append(kv, "op.max_attempts", start.MaxAttempts)
	}
	if len(kv) >= 2 {
		Add(ctx, kv[0].(string), kv[1], kv[2:]...)
	}
}

func annotateOperationFailures(ctx context.Context, err error, recovered any) {
	if recovered != nil {
		Add(ctx, "panic", map[string]any{
			"type":  fmt.Sprintf("%T", recovered),
			"value": fmt.Sprint(recovered),
		})
	}

	switch {
	case err != nil:
		Error(ctx, err)
	case recovered != nil:
		Error(ctx, fmt.Errorf("panic: %v", recovered))
	}
}

func resolveOutcome(result OperationResult) Outcome {
	if isValidOutcome(result.Outcome) {
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

func buildSampleInput(event *Event, start OperationStart, result OperationResult, duration time.Duration, outcome Outcome, level Level) SampleInput {
	fields := EventFields(event)

	method, _ := fields["http.method"].(string)
	path, _ := fields["http.path"].(string)
	statusCode := result.Code
	if v, ok := fields["http.status"]; ok {
		if parsed, ok := asInt(v); ok {
			statusCode = parsed
		}
	}

	hasError := EventHasError(event) || result.Code >= 500 || outcome != OutcomeSuccess

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
	fields := EventFields(event)
	if len(fields) == 0 {
		return start
	}

	if start.Domain == "" {
		if v, ok := fields["op.domain"].(string); ok && v != "" {
			start.Domain = Domain(v)
		}
	}
	if start.Name == "" {
		if v, ok := fields["op.name"].(string); ok && v != "" {
			start.Name = v
		}
	}
	if start.ID == "" {
		if v, ok := fields["op.id"].(string); ok {
			start.ID = v
		}
	}
	if start.Source == "" {
		if v, ok := fields["op.source"].(string); ok {
			start.Source = v
		}
	}
	if start.Attempt == 0 {
		if v, ok := asInt(fields["op.attempt"]); ok {
			start.Attempt = v
		}
	}
	if start.MaxAttempts == 0 {
		if v, ok := asInt(fields["op.max_attempts"]); ok {
			start.MaxAttempts = v
		}
	}
	return start
}

func shouldWriteOperation(cfg Config, policy OperationPolicy, in SampleInput) bool {
	if cfg.Sampler != nil {
		return cfg.Sampler(in)
	}

	if in.HasError || in.Code >= 500 || in.StatusCode >= 500 || in.Outcome != OutcomeSuccess {
		return true
	}

	rate := clampRate(cfg.SamplingRate)
	if policy.SamplingRate != nil {
		rate = clampRate(*policy.SamplingRate)
	} else if levelRate, ok := levelSamplingRate(cfg.LevelSamplingRates, in.Level); ok {
		rate = levelRate
	}
	return shouldSample(rate)
}

func resolveMessage(configured string, domain Domain) string {
	if configured != "" {
		return configured
	}
	if normalizeDomain(domain) == DomainHTTP {
		return defaultMessage
	}
	return defaultOperationMessage
}

func resolveEventMessage(configured string, domain Domain, event *Event) string {
	if EventHasMessage(event) {
		return EventMessage(event)
	}
	return resolveMessage(configured, domain)
}

func normalizeDomain(domain Domain) Domain {
	if domain == "" {
		return defaultDomainValue
	}
	return domain
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	default:
		return 0, false
	}
}

func policyForDomain(cfg Config, domain Domain) OperationPolicy {
	if cfg.OperationPolicies == nil {
		return OperationPolicy{}
	}
	policy, ok := cfg.OperationPolicies[normalizeDomain(domain)]
	if !ok {
		return OperationPolicy{}
	}
	return policy
}

func defaultPolicy() OperationPolicy {
	return OperationPolicy{
		SuccessLevel: LevelInfo,
		FailureLevel: LevelError,
		PanicLevel:   LevelError,
	}
}

func levelFromPolicy(policy OperationPolicy, outcome Outcome) Level {
	def := defaultPolicy()
	if outcomeLevel, ok := policy.OutcomeLevels[outcome]; ok && isValidLevel(outcomeLevel) {
		return outcomeLevel
	}

	successLevel := def.SuccessLevel
	if isValidLevel(policy.SuccessLevel) {
		successLevel = policy.SuccessLevel
	}
	failureLevel := def.FailureLevel
	if isValidLevel(policy.FailureLevel) {
		failureLevel = policy.FailureLevel
	}
	panicLevel := def.PanicLevel
	if isValidLevel(policy.PanicLevel) {
		panicLevel = policy.PanicLevel
	}

	switch outcome {
	case OutcomeSuccess:
		return successLevel
	case OutcomePanic:
		return panicLevel
	default:
		return failureLevel
	}
}

func mergeLevelWithFloor(autoLevel, requestedLevel Level, hasRequested bool) Level {
	if !hasRequested || !isValidLevel(requestedLevel) {
		return autoLevel
	}
	if levelRank(requestedLevel) > levelRank(autoLevel) {
		return requestedLevel
	}
	return autoLevel
}

func levelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 10
	case LevelInfo:
		return 20
	case LevelWarn:
		return 30
	case LevelError:
		return 40
	default:
		return 20
	}
}

func clampRate(rate float64) float64 {
	if rate < 0 {
		return 0
	}
	if rate > 1 {
		return 1
	}
	return rate
}

func levelSamplingRate(rates map[Level]float64, level Level) (float64, bool) {
	if rates == nil {
		return 0, false
	}
	rate, ok := rates[level]
	if !ok {
		return 0, false
	}
	return clampRate(rate), true
}

func isValidOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry:
		return true
	default:
		return false
	}
}
