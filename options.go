package hc

const (
	// DefaultMessage is the fallback final message for HTTP request events.
	DefaultMessage = "request_completed"
	// DefaultOperationMessage is the fallback final message for non-HTTP operation events.
	DefaultOperationMessage = "operation_completed"
)

// Config controls event finalization behavior.
type Config struct {
	// Sink receives the finalized event.
	Sink Sink

	// SamplingRate controls random sampling for non-error requests in [0,1]. Default is 1.0.
	// 0.0 means no sampling, 1.0 means full sampling.
	SamplingRate float64

	// LevelSamplingRates optionally overrides SamplingRate by final log level.
	// Values are clamped into [0,1].
	LevelSamplingRates map[Level]float64

	// Sampler overrides built-in sampling when set.
	// Return true to keep and write the event.
	Sampler Sampler

	// OperationPolicies optionally customizes non-HTTP operation behavior by domain.
	// A domain SamplingRate overrides generic level/default sampling rates.
	OperationPolicies map[Domain]OperationPolicy

	// Message is the final log message.
	Message string
}
