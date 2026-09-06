package hc

// Domain identifies the operation category.
type Domain string

const (
	DomainHTTP    Domain = "http" // the middlewares; http.status is canonical
	DomainJob     Domain = "job"  // worker integration; op.code is canonical
	DomainMessage Domain = "msg"  // custom message-consumer operations
	DomainCLI     Domain = "cli"  // command-line invocations
)

// Outcome describes operation completion status.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"  // default for anything not below
	OutcomeFailure  Outcome = "failure"  // error, or 5xx canonical code
	OutcomePanic    Outcome = "panic"    // recovered panic (re-panicked)
	OutcomeCanceled Outcome = "canceled" // error wrapping context.Canceled
	OutcomeTimeout  Outcome = "timeout"  // error wrapping context.DeadlineExceeded
	OutcomeRetry    Outcome = "retry"    // explicit-input only: resolveOutcome never emits it
)

// OperationPolicy customizes lifecycle defaults per domain. Zero
// fields mean the defaults: success logs at INFO, failures and panics
// at ERROR; OutcomeLevels (checked first) and then the three level
// fields override that, per outcome or per class. SamplingRate, when
// set, replaces both the global rate and any LevelSamplingRates entry
// for the domain.
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
