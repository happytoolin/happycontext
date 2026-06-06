package hc

import "context"

const defaultDomainValue Domain = "operation"

const defaultOpName = "operation"

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

type operationResult struct {
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

// IsValidOutcome reports whether outcome is a valid operation outcome.
func IsValidOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry:
		return true
	default:
		return false
	}
}
