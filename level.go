package hc

// IsValidLevel reports whether level is a valid severity level.
func IsValidLevel(level Level) bool {
	switch level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		return true
	default:
		return false
	}
}
