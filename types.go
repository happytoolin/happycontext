package hc

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

// IsValidOutcome reports whether outcome is a valid operation outcome.
func IsValidOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomePanic, OutcomeCanceled, OutcomeTimeout, OutcomeRetry:
		return true
	default:
		return false
	}
}

const (
	// DefaultMessage is the fallback final message for HTTP request events.
	DefaultMessage = "request_completed"
	// DefaultOperationMessage is the fallback final message for non-HTTP operation events.
	DefaultOperationMessage = "operation_completed"
)
