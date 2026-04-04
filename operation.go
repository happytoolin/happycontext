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

// BeginOperation initializes context/event and operation envelope metadata.
func BeginOperation(baseCtx context.Context, start OperationStart) (context.Context, *Event) {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, event := NewContext(baseCtx)
	applyOperationStartFields(ctx, start)
	return ctx, event
}

// FinishOperation finalizes and writes an operation event.
func FinishOperation(cfg Config, in OperationFinish) bool {
	if cfg.Sink == nil || in.Event == nil || in.Ctx == nil {
		return false
	}

	cfg = normalizeOperationConfig(cfg)
	start := hydrateOperationStart(in.Start, in.Event)
	policy := policyForDomain(cfg, start.Domain)

	applyOperationStartFields(in.Ctx, start)
	annotateOperationFailures(in.Ctx, in.Err, in.Recovered)

	duration := time.Since(EventStartTime(in.Event))
	Add(in.Ctx, "duration_ms", duration.Milliseconds(), "op.code", in.Code)

	outcome := resolveOutcome(in)
	Add(in.Ctx, "op.outcome", string(outcome))

	autoLevel := levelFromPolicy(policy, outcome)
	requestedLevel, hasRequestedLevel := GetLevel(in.Ctx)
	level := mergeLevelWithFloor(autoLevel, requestedLevel, hasRequestedLevel)

	sampleIn := buildSampleInput(in, start, duration, outcome, level)
	if !shouldWriteOperation(cfg, policy, sampleIn) {
		return false
	}

	cfg.Sink.Write(level, resolveEventMessage(cfg.Message, start.Domain, in.Event), EventFields(in.Event))
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

func resolveOutcome(in OperationFinish) Outcome {
	if isValidOutcome(in.Outcome) {
		return in.Outcome
	}
	if in.Recovered != nil {
		return OutcomePanic
	}
	if in.Err != nil {
		switch {
		case errors.Is(in.Err, context.Canceled):
			return OutcomeCanceled
		case errors.Is(in.Err, context.DeadlineExceeded):
			return OutcomeTimeout
		default:
			return OutcomeFailure
		}
	}
	if in.Code >= 500 {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

func buildSampleInput(in OperationFinish, start OperationStart, duration time.Duration, outcome Outcome, level Level) SampleInput {
	fields := EventFields(in.Event)

	method, _ := fields["http.method"].(string)
	path, _ := fields["http.path"].(string)
	statusCode := in.Code
	if v, ok := fields["http.status"]; ok {
		if parsed, ok := asInt(v); ok {
			statusCode = parsed
		}
	}

	hasError := EventHasError(in.Event) || in.Code >= 500 || outcome != OutcomeSuccess

	name := start.Name
	if name == "" {
		name = defaultOpName
	}

	return SampleInput{
		Domain:     normalizeDomain(start.Domain),
		Operation:  name,
		Outcome:    outcome,
		Code:       in.Code,
		Method:     method,
		Path:       path,
		StatusCode: statusCode,
		Duration:   duration,
		Level:      level,
		HasError:   hasError,
		Event:      in.Event,
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

	rate := cfg.SamplingRate
	if policy.SamplingRate != nil {
		rate = *policy.SamplingRate
	} else if cfg.LevelSamplingRates != nil {
		if levelRate, ok := cfg.LevelSamplingRates[in.Level]; ok {
			rate = levelRate
		}
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
		return defaultPolicy()
	}
	policy, ok := cfg.OperationPolicies[normalizeDomain(domain)]
	if !ok {
		return defaultPolicy()
	}
	return policy
}

func normalizeOperationConfig(cfg Config) Config {
	if cfg.SamplingRate < 0 {
		cfg.SamplingRate = 0
	}
	if cfg.SamplingRate > 1 {
		cfg.SamplingRate = 1
	}

	if len(cfg.LevelSamplingRates) > 0 {
		clamped := make(map[Level]float64, len(cfg.LevelSamplingRates))
		for level, rate := range cfg.LevelSamplingRates {
			if !isValidLevel(level) {
				continue
			}
			clamped[level] = clampRate(rate)
		}
		cfg.LevelSamplingRates = clamped
	}

	if len(cfg.OperationPolicies) > 0 {
		normalized := make(map[Domain]OperationPolicy, len(cfg.OperationPolicies))
		for domain, policy := range cfg.OperationPolicies {
			normalized[normalizeDomain(domain)] = normalizePolicy(policy)
		}
		cfg.OperationPolicies = normalized
	}

	return cfg
}

func normalizePolicy(policy OperationPolicy) OperationPolicy {
	def := defaultPolicy()

	if !isValidLevel(policy.SuccessLevel) {
		policy.SuccessLevel = def.SuccessLevel
	}
	if !isValidLevel(policy.FailureLevel) {
		policy.FailureLevel = def.FailureLevel
	}
	if !isValidLevel(policy.PanicLevel) {
		policy.PanicLevel = def.PanicLevel
	}

	if len(policy.OutcomeLevels) > 0 {
		outcomeLevels := make(map[Outcome]Level, len(policy.OutcomeLevels))
		for outcome, level := range policy.OutcomeLevels {
			if !isValidOutcome(outcome) || !isValidLevel(level) {
				continue
			}
			outcomeLevels[outcome] = level
		}
		policy.OutcomeLevels = outcomeLevels
	}

	if policy.SamplingRate != nil {
		rate := clampRate(*policy.SamplingRate)
		policy.SamplingRate = &rate
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
	if outcomeLevel, ok := policy.OutcomeLevels[outcome]; ok && isValidLevel(outcomeLevel) {
		return outcomeLevel
	}

	switch outcome {
	case OutcomeSuccess:
		return policy.SuccessLevel
	case OutcomePanic:
		return policy.PanicLevel
	default:
		return policy.FailureLevel
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

func isValidOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry:
		return true
	default:
		return false
	}
}
