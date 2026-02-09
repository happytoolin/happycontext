package happycontext

import "context"

const (
	// LevelDebug represents debug-level severity.
	LevelDebug = "DEBUG"
	// LevelInfo represents info-level severity.
	LevelInfo = "INFO"
	// LevelWarn represents warn-level severity.
	LevelWarn = "WARN"
	// LevelError represents error-level severity.
	LevelError = "ERROR"
)

// Sink receives finalized request events.
type Sink interface {
	Write(ctx context.Context, level, message string, fields map[string]any)
}
