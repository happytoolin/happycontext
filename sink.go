package hc

// Level represents event severity.
type Level string

const (
	// LevelDebug represents debug-level severity.
	LevelDebug Level = "DEBUG"
	// LevelInfo represents info-level severity.
	LevelInfo Level = "INFO"
	// LevelWarn represents warn-level severity.
	LevelWarn Level = "WARN"
	// LevelError represents error-level severity.
	LevelError Level = "ERROR"
)

// Sink receives finalized request events.
type Sink interface {
	Write(level Level, message string, fields map[string]any)
}
